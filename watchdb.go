package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"container/list"
	"crypto/md5"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/howeyc/fsnotify"
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("watchdb")

var clients = make(chan chan string, 1)
var channels = list.New()

var sqlite_path string

func main() {
	usage := `watchdb

Usage:
  watchdb watch [options] <db.sql>
  watchdb sync [options] <remote> <db.sql>

Options:
  -h --help               Show this screen
  -v --version            Show version
  -c --config-file=<file> watchdb config file (optional)
  -a --bind-addr=<addr>   Address to bind to (default 0.0.0.0)
  -p --bind-port=<port>   Port to bind to (default 8144)
  -i --sync-interval=<ms> Notify slaves at most every X milliseconds (default 1000)
  --no-backup             Don't create a backup file prior to sync
  -s --ssl                Use https for connecting to watcher (recommended)
  --ssl-key-file=<file>   SSL private key file to use for encrypted connections (will be generated if not provided)
  --ssl-cert-file=<file>  SSL certificate file to use for encrypted connections (will be generated if not provided)
  --ssl-skip-verify       Don't verify SSL certificate (required if self-signed or auto-generated)
  --auth-key=<auth-key>   Auth key to be sent (or required) with all connections
`

	arguments, err := docopt.Parse(usage, nil, true, "0.1", false)
	if err != nil {
		fmt.Println(err)
		fmt.Println(usage)
		return
	}

	var format = logging.MustStringFormatter(
		"%{time:2006-01-02 15:04:05.000} | %{color}%{message}%{color:reset}",
	)

	logbackend := logging.NewLogBackend(os.Stderr, "", 0)
	logformatter := logging.NewBackendFormatter(logbackend, format)
	logging.SetBackend(logformatter)

	sqlite_path = determineSqlitePath()
	options := loadConfig(arguments)

	log.Info("starting watchdb")

	if arguments["watch"].(bool) {
		path := options.SyncFile
		path_exists, err := exists(path)

		if err != nil || !path_exists {
			log.Error("can't watch '%s', file not found", path)
			return
		}

		addr := fmt.Sprintf("%s:%s", options.BindAddr, options.BindPort)

		go listen(addr, path, options)
		watch(path, options)
	} else if arguments["sync"].(bool) {
		path, ok := arguments["<db.sql>"].(string)

		if !ok {
			path = "synced.sql"
		}

		connect_addr := options.RemoteConn

		sync(connect_addr, path, options)
	}
}

func createWatchDBDir() string {
	homedir := os.Getenv("HOME")
	watchdb_dir := path.Join(homedir, ".config", "watchdb")
	watchdb_bin_dir := path.Join(watchdb_dir, "bin")

	err := os.MkdirAll(watchdb_bin_dir, 0700)
	if err != nil {
		log.Fatalf("unable to create watchdb main directory: %s", err)
	}

	return watchdb_dir
}

func determineSqlitePath() string {
	watchdb_dir := createWatchDBDir()

	data, err := Asset("sqlite3")
	if err == nil {
		sqlite3_path := path.Join(watchdb_dir, "bin", "sqlite3")
		err = ioutil.WriteFile(sqlite3_path, data, 0770)
		if err != nil {
			log.Fatalf("unable to expand sqlite3 into bin directory: %s", err)
		}

		log.Debug("using embedded sqlite3")
		return sqlite3_path
	}

	data, err = Asset("sqlite3.exe")
	if err == nil {
		sqlite3_exe_path := path.Join(watchdb_dir, "bin", "sqlite3.exe")
		err = ioutil.WriteFile(sqlite3_exe_path, data, 0770)
		if err != nil {
			log.Fatalf("unable to expand sqlite3.exe into bin directory: %s", err)
		}

		log.Debug("using embedded sqlite3.exe")
		return sqlite3_exe_path
	}

	envpath := os.Getenv("PATH")
	path_list := strings.Split(envpath, ":")

	var sqlite3_path_path string
	for _, path_entry := range path_list {
		sqlite_exists, _ := exists(path.Join(path_entry, "sqlite3"))
		sqlite_exe_exists, _ := exists(path.Join(path_entry, "sqlite3.exe"))

		if sqlite_exists {
			sqlite3_path_path = path.Join(path_entry, "sqlite3")
			break
		} else if sqlite_exe_exists {
			sqlite3_path_path = path.Join(path_entry, "sqlite3.exe")
			break
		}
	}

	if sqlite3_path_path != "" {
		log.Debug("using sqlite3 in PATH: %s", sqlite3_path_path)
		return sqlite3_path_path
	} else {
		log.Fatal("unable to find sqlite3 embedded or in PATH")
	}

	return ""
}

func sendMessage(message string) {
	for e := channels.Front(); e != nil; e = e.Next() {
		e.Value.(chan string) <- message
	}
}

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

func listen(addr string, path string, options WatchConfig) {
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
		if options.AuthKey != "" {
			provided_key := r.Header.Get("Authorization")
			if provided_key != options.AuthKey {
				log.Warning("rejected connection from %s, incorrect auth key provided: '%s'", r.RemoteAddr, provided_key)
				http.Error(w, "authorization required", 401)
				return
			}
		}

		log.Debug("sending DB to " + r.RemoteAddr)

		out, err := exec.Command(sqlite_path, path, ".dump").Output()
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
		if options.AuthKey != "" {
			provided_key := r.Header.Get("Authorization")
			if provided_key != options.AuthKey {
				log.Warning("rejected connection from %s, incorrect auth key provided: '%s'", r.RemoteAddr, provided_key)
				http.Error(w, "authorization required", 401)
				return
			}
		}

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

	if options.UseSSL {
		log.Notice("listening for SSL connections on " + addr)
		log.Fatal(http.ListenAndServeTLS(addr, options.SSLCertFile, options.SSLKeyFile, nil))
	} else {
		log.Notice("listening on " + addr)
		log.Fatal(http.ListenAndServe(addr, nil))
	}
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

func watch(path string, options WatchConfig) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	db_md5 := getMD5(path)
	done := make(chan bool)

	needs_update := make(chan bool, 1)

	go func() {
		for {
			<-needs_update

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

			time.Sleep(time.Duration(options.SyncInterval) * time.Millisecond)
		}
	}()

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
					select {
					case needs_update <- true:
						// queued successfully
					default:
						// request is already in line, don't queue another one
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

func sync(addr string, path string, options WatchConfig) {
	var poll_url string
	var download_url string

	if options.UseSSL {
		poll_url = fmt.Sprintf("https://%s/watch", addr)
		download_url = fmt.Sprintf("https://%s/latest", addr)
	} else {
		poll_url = fmt.Sprintf("http://%s/watch", addr)
		download_url = fmt.Sprintf("http://%s/latest", addr)
	}

	done := make(chan bool)
	download := make(chan bool, 1)

	sql_backup_path := fmt.Sprintf("%s.new.sql", path)
	backup_path := fmt.Sprintf("%s.old", path)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: options.SkipSSLVerify},
	}
	client := &http.Client{Transport: tr}

	go func() {
		for {
			<-download

			req, err := http.NewRequest("GET", download_url, nil)
			if options.AuthKey != "" {
				req.Header.Add("Authorization", options.AuthKey)
			}

			resp, err := client.Do(req)

			if err != nil {
				log.Warning("unable to download latest DB from upstream, retrying in 5s: %s", err)

				go func() {
					time.Sleep(time.Duration(5) * time.Second)
					select {
					case download <- true:
						// add a download to the queue
					default:
						// already a download in queue, don't add another one
					}
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
			drop_sqlite_out, err := exec.Command(sqlite_path, path, drop_sql_command).CombinedOutput()
			if err != nil {
				log.Error("unable to drop existing DB prior to import, output: %s", string(drop_sqlite_out))
				continue
			}

			sql_command := fmt.Sprintf(".read %s", sql_backup_path)
			sqlite_out, err := exec.Command(sqlite_path, path, sql_command).CombinedOutput()
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
		initial_sync_done := false

		for {
			not_successful := false

			go func() {
				time.Sleep(time.Duration(400) * time.Millisecond)

				if !not_successful {
					log.Notice("connected to upstream")

					if !initial_sync_done {
						initial_sync_done = true

						if path_exists, _ := exists(path); path_exists && !options.NoBackup {
							orig_backup_path := fmt.Sprintf("%s.orig", path)
							err := copyFileContents(path, orig_backup_path)
							if err != nil {
								log.Fatal("unable to back up current sqlite database: %s", err)
							}

							log.Notice("syncing upstream DB to %s", path)
							log.Info("if that's not what you meant to do, we've saved a backup at %s", orig_backup_path)
						}

						log.Notice("running initial sync")
						download <- true
					}
				}
			}()

			req, err := http.NewRequest("GET", poll_url, nil)
			if options.AuthKey != "" {
				req.Header.Add("Authorization", options.AuthKey)
			}

			resp, err := client.Do(req)

			if err != nil {
				not_successful = true

				if strings.Contains(err.Error(), "malformed HTTP response") {
					log.Error("it looks like the upstream server is using SSL, did you forget to specify --ssl?")

					os.Exit(1)
				}

				if strings.Contains(err.Error(), "certificate signed by unknown authority") {
					log.Error("encountered a certificate error when trying to verify SSL, you may want to use --ssl-skip-verify if this is a self-signed certificate")

					os.Exit(1)
				}

				if strings.Contains(err.Error(), "oversized record received") && options.UseSSL {
					log.Error("it looks like you're trying to connect through SSL, but the server isn't set up to use SSL, try removing --ssl or properly setting it up on the server")

					os.Exit(1)
				}

				log.Warning("unable to watch for upstream updates: %s", err)

				time.Sleep(time.Duration(5) * time.Second)
				continue
			}

			defer resp.Body.Close()

			body, err := ioutil.ReadAll(resp.Body)

			if err != nil {
				log.Warning("unable to parse upstream body: %s", err)
				not_successful = true
				time.Sleep(time.Duration(5) * time.Second)
				continue
			}

			if resp.StatusCode == 401 {
				if options.AuthKey == "" {
					log.Error("upstream requires an authentication key to connect, provide via --auth-key")
				} else {
					log.Error("authentication key '%s' rejected by server, make sure it was entered correctly", options.AuthKey)
				}

				done <- true
				not_successful = true

				return
			}

			if strings.TrimSpace(string(body)) == "modified" {
				select {
				case download <- true:
					// add a download to the queue
				default:
					// already a download in queue, don't add another one
				}

			} else {
				log.Warning("unknown body received from upstream: %s", body)
				not_successful = true
				time.Sleep(time.Duration(5) * time.Second)
				continue
			}
		}
	}()

	<-done
}
