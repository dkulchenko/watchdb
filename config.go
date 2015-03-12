package main

import (
	"github.com/dkulchenko/watchdb/ssl"
	"io/ioutil"

	yaml "gopkg.in/yaml.v2"
)

type WatchConfig struct {
	BindAddr string `yaml:"bind_addr,omitempty"`
	BindPort string `yaml:"bind_port,omitempty"`

	NoBackup bool `yaml:"no_backup,omitempty"`

	UseSSL        bool   `yaml:"use_ssl,omitempty"`
	SSLKeyFile    string `yaml:"ssl_key_file,omitempty"`
	SSLCertFile   string `yaml:"ssl_cert_file,omitempty"`
	SkipSSLVerify bool   `yaml:"skip_ssl_verify,omitempty"`

	AuthKey string `yaml:"auth_key,omitempty"`

	SyncFile   string `yaml:"sync_file,omitempty"`
	RemoteConn string `yaml:"remote_conn,omitempty"`
}

func loadConfig(arguments map[string]interface{}) WatchConfig {
	initialConfig := WatchConfig{
		BindAddr:      "0.0.0.0",
		BindPort:      "8144",
		NoBackup:      false,
		UseSSL:        false,
		SkipSSLVerify: false,
	}

	config_file, ok := arguments["--config-file"].(string)
	if ok {
		config, err := ioutil.ReadFile(config_file)
		if err != nil {
			log.Fatalf("error reading config file: %v", err)
		}

		err = yaml.Unmarshal(config, &initialConfig)
		if err != nil {
			log.Fatalf("error reading config file: %v", err)
		}

		log.Notice("config file loaded")
	}

	if bindaddr, ok := arguments["--bind-addr"].(string); ok {
		initialConfig.BindAddr = bindaddr
	}

	if bindport, ok := arguments["--bind-port"].(string); ok {
		initialConfig.BindPort = bindport
	}

	if nobackup, ok := arguments["--bind-port"].(bool); ok {
		initialConfig.NoBackup = nobackup
	}

	if usessl, ok := arguments["--ssl"].(bool); ok {
		initialConfig.UseSSL = usessl
	}

	if sslkeyfile, ok := arguments["--ssl-key-file"].(string); ok {
		initialConfig.SSLKeyFile = sslkeyfile
	}

	if sslcertfile, ok := arguments["--ssl-cert-file"].(string); ok {
		initialConfig.SSLCertFile = sslcertfile
	}

	if skipsslverify, ok := arguments["--ssl-skip-verify"].(bool); ok {
		initialConfig.SkipSSLVerify = skipsslverify
	}

	if authkey, ok := arguments["--auth-key"].(string); ok {
		initialConfig.AuthKey = authkey
	}

	if syncfile, ok := arguments["<db.sql>"].(string); ok {
		initialConfig.SyncFile = syncfile
	}

	if remoteconn, ok := arguments["<remote>"].(string); ok {
		initialConfig.RemoteConn = remoteconn
	}

	watch, ok := arguments["watch"].(bool)

	if watch && initialConfig.UseSSL && (initialConfig.SSLKeyFile == "" || initialConfig.SSLCertFile == "") {
		log.Warning("ssl cert file and key file weren't specified, automatically generating")
		ssl.GenerateSelfSignedCerts()
		initialConfig.SSLKeyFile = "/tmp/watchdb-key.pem"
		initialConfig.SSLCertFile = "/tmp/watchdb-cert.pem"
	}

	return initialConfig
}
