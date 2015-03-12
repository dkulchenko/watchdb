package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"container/list"
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/howeyc/fsnotify"
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("watchdb")

var clients = make(chan chan string, 1)
var channels = list.New()

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func main() {
	usage := `watchdb

Usage:
  watchdb watch [options] <db.sql>
  watchdb sync [options] <remote> <db.sql>

Options
  -h --help    Show this screen
  --version    Show version
  --bind-addr  Address to bind to (default 0.0.0.0)
  --bind-port  Port to bind to (default 8144)
  --no-backup  Don't create a backup file prior to sync
`

	arguments, err := docopt.Parse(usage, nil, true, "0.1", false)
	if err != nil {
		fmt.Println(arguments)
		return
	}

	var format = logging.MustStringFormatter(
		"%{time:2006-01-02 15:04:05.000} | %{color}%{message}%{color:reset}",
	)

	logbackend := logging.NewLogBackend(os.Stderr, "", 0)
	logformatter := logging.NewBackendFormatter(logbackend, format)
	logging.SetBackend(logformatter)

	log.Info("starting watchdb")

	if arguments["watch"].(bool) {
		path := arguments["<db.sql>"].(string)
		path_exists, err := exists(path)

		if err != nil || !path_exists {
			log.Error("can't watch '%s', file not found", path)
			return
		}

		bind_addr := "0.0.0.0"
		if _, ok := arguments["bind-addr"].(string); ok {
			bind_addr = arguments["bind-addr"].(string)
		}

		bind_port := "8144"
		if _, ok := arguments["bind-port"].(string); ok {
			bind_port = arguments["bind-port"].(string)
		}

		addr := fmt.Sprintf("%s:%s", bind_addr, bind_port)

		go listen(addr, path)
		watch(path, arguments)
	} else if arguments["sync"].(bool) {
		path, ok := arguments["<db.sql>"].(string)

		if !ok {
			path = "synced.sql"
		}

		connect_addr := arguments["<remote>"].(string)

		sync(connect_addr, path, arguments)
	}
}

func sendMessage(message string) {
	for e := channels.Front(); e != nil; e = e.Next() {
		e.Value.(chan string) <- message
	}
}

func listen(addr string, path string) {
	removeclients := make(chan *list.Element, 1)
	listelement := make(chan *list.Element, 1)

	go func() {
		for {
			select {
			case c := <-clients:
				listelement <- channels.PushBack(c)
			case e := <-removeclients:
				channels.Remove(e)
			}
		}
	}()

	http.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		log.Debug("sending DB to " + r.RemoteAddr)

		out, err := exec.Command("sqlite3", path, ".dump").Output()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || len(out) < 1000 {
			w.Write(out)
		} else {
			w.Header().Set("Content-Encoding", "gzip")

			var b bytes.Buffer
			gz := gzip.NewWriter(&b)
			if _, err := gz.Write(out); err != nil {
				log.Error("unable to gzip sqlite output: ", err)
				http.Error(w, err.Error(), 500)
				return
			}
			if err := gz.Flush(); err != nil {
				log.Error("unable to gzip sqlite output: ", err)
				http.Error(w, err.Error(), 500)
				return
			}
			if err := gz.Close(); err != nil {
				log.Error("unable to gzip sqlite output: ", err)
				http.Error(w, err.Error(), 500)
				return
			}

			w.Write(b.Bytes())
		}
	})

	http.HandleFunc("/watch", func(w http.ResponseWriter, r *http.Request) {
		log.Debug("remote syncer " + r.RemoteAddr + " connected")

		notify := w.(http.CloseNotifier).CloseNotify()

		message := make(chan string, 1)
		clients <- message

		element := <-listelement

		select {
		case <-notify:
			log.Debug("remote syncer " + r.RemoteAddr + " disconnected")
			removeclients <- element
		case msg := <-message:
			io.WriteString(w, msg)
			log.Debug("remote syncer " + r.RemoteAddr + " disconnected")
			removeclients <- element
		}
	})

	log.Notice("listening on " + addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func getMD5(path string) string {
	h := md5.New()

	file, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		io.WriteString(h, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	return string(h.Sum([]byte{}))
}

func watch(path string, options map[string]interface{}) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	db_md5 := getMD5(path)
	done := make(chan bool)

	go func() {
		for {
			select {
			case ev := <-watcher.Event:
				if ev.IsDelete() {
					log.Warning("watched DB was deleted, exiting")
					done <- true
				}

				if ev.IsRename() {
					log.Warning("watched DB was renamed, watching may no longer work")
				}

				if ev.IsModify() {
					new_md5 := getMD5(path)
					if db_md5 == new_md5 {
						log.Debug("watched DB was modified, but checksum is the same, not notifying clients")
					} else {
						db_md5 = new_md5
						if channels.Len() < 1 {
							log.Info("watched DB was modified, but no clients to notify")
						} else {
							log.Info("watched DB was modified, notifying connected clients (%d)", channels.Len())
							sendMessage("modified\n")
						}
					}
				}
			case err := <-watcher.Error:
				log.Error("error watching file: %s", err)
				done <- true
			}
		}
	}()

	log.Notice("watching %s", path)

	err = watcher.Watch(path)
	if err != nil {
		log.Fatal(err)
	}

	<-done

	watcher.Close()
}

func copyFileContents(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return
	}
	err = out.Sync()
	return
}

func sync(addr string, path string, options map[string]interface{}) {
	poll_url := fmt.Sprintf("http://%s/watch", addr)
	download_url := fmt.Sprintf("http://%s/latest", addr)

	done := make(chan bool)
	download := make(chan bool)

	sql_backup_path := fmt.Sprintf("%s.new.sql", path)
	backup_path := fmt.Sprintf("%s.old", path)

	go func() {
		for {
			<-download

			resp, err := http.Get(download_url)

			if err != nil {
				log.Warning("unable to download latest DB from upstream, retrying in 2s: %s", err)

				go func() {
					time.Sleep(time.Duration(2) * time.Second)
					download <- true
				}()

				continue
			}

			_ = os.Remove(sql_backup_path)
			out, err := os.Create(sql_backup_path)
			if err != nil {
				log.Error("unable to write latest DB to file: %s", err)
				continue
			}

			defer out.Close()
			io.Copy(out, resp.Body)

			if dbexists, _ := exists(path); dbexists {
				err = copyFileContents(path, backup_path)
				if err != nil {
					log.Error("unable to back up current sqlite database: %s", err)
					continue
				}

				err = os.Chmod(path, 0600)
				if err != nil {
					log.Error("unable to change permissions on existing DB prior to import, err: %s", err)
					continue
				}
			}

			drop_sql_command := "PRAGMA writable_schema = 1; delete from sqlite_master where type in ('table', 'index', 'trigger'); PRAGMA writable_schema = 0; VACUUM; PRAGMA INTEGRITY_CHECK;"
			drop_sqlite_out, err := exec.Command("sqlite3", path, drop_sql_command).CombinedOutput()
			if err != nil {
				log.Error("unable to import drop existing DB prior to import, output: %s", string(drop_sqlite_out))
				continue
			}

			sql_command := fmt.Sprintf(".read %s", sql_backup_path)
			sqlite_out, err := exec.Command("sqlite3", path, sql_command).CombinedOutput()
			if err != nil {
				log.Error("unable to import newly downloaded DB from upstream, output: %s", string(sqlite_out))
				continue
			}

			err = os.Chmod(path, 0400)
			if err != nil {
				log.Error("unable to change permissions on DB following import, err: %s", err)
				continue
			}

			log.Info("updated DB on disk with latest")

			_ = os.Remove(sql_backup_path)
		}
	}()

	go func() {
		for {
			not_successful := false

			go func() {
				time.Sleep(time.Duration(500) * time.Millisecond)

				if !not_successful {
					log.Notice("connected to upstream")
				}
			}()

			resp, err := http.Get(poll_url)

			if err != nil {
				log.Warning("unable to watch for upstream updates: %s", err)
				not_successful = true
				time.Sleep(time.Duration(2) * time.Second)
				continue
			}

			defer resp.Body.Close()

			body, err := ioutil.ReadAll(resp.Body)

			if err != nil {
				log.Warning("unable to parse upstream body: %s", err)
				time.Sleep(time.Duration(2) * time.Second)
				continue
			}

			if strings.TrimSpace(string(body)) == "modified" {
				download <- true
			} else {
				log.Warning("unknown body received from upstream: %s", body)
			}
		}
	}()

	log.Notice("running initial sync")

	if path_exists, _ := exists(path); path_exists {
		orig_backup_path := fmt.Sprintf("%s.orig", path)
		err := copyFileContents(path, orig_backup_path)
		if err != nil {
			log.Error("unable to back up current sqlite database: %s", err)
			return
		}

		log.Notice("replacing contents of %s with upstream DB", path)
		log.Info("if that's not what you meant to do, we've saved a backup at %s", orig_backup_path)
	}

	download <- true

	<-done
}
