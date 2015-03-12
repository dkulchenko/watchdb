# watchdb

A tool for keeping SQLite databases in sync across a network.

## Overview

watchdb is a tool that enables quick setup of master-slave synchronization for 
SQLite databases across a network.

Synchronization is one-way only (so changes made on the master will overwrite
changes made on slaves). Slave databases are kept read-only to prevent 
accidental writes.

## Features

- Works on Linux, OS X, Windows
- No dependencies other than 'sqlite3' in $PATH
- Replicates sqlite DB changes quickly across any number of nodes
- No changes to application code required
- Optional authentication and encryption supported

In the future:

- Incremental (partial) DB syncing

## Installation

[Precompiled binaries](https://github.com/dkulchenko/watchdb/releases) for supported 
operating systems are available. You'll need a working sqlite3 binary in $PATH.

Alternatively, run `go get github.com/dkulchenko/watchdb`.

## Usage

Watch a SQLite database:

```
watchdb watch mydb.sqlite
```

And on a separate server, sync:

```
watchdb sync 127.0.0.1:8144 mydbcopy.sqlite
```

### Options

Use a configuration file (an example file is available at config/example.yml):

```
watchdb watch --config-file=config.yml mydb.sqlite
```

Set a custom bind address/port:

```
watchdb watch --bind-addr=127.0.0.1 --bind-port=1234 watch mydb.sqlite
```

### Authentication

Require an auth key to be sent before syncing is allowed (similar to Redis AUTH):

```
watchdb watch --auth-key 927bc430fc2195fa2f0caaf35d115c watch mydb.sqlite
```

and on the client:

```
watchdb sync --auth-key 927bc430fc2195fa2f0caaf35d115c sync 127.0.0.1:8144 mydbcopy.sqlite
```

Or specify it in the configuration file using the `auth_key` parameter.

### Encryption

watchdb supports SSL for encrypted syncing between nodes.

```
watchdb watch --ssl --ssl-key-file=key.pem --ssl-cert-file=key.crt mydb.sqlite
```

and on the client:

```
watchdb watch --ssl 127.0.0.1:8144 mydbcopy.sqlite
```

You may omit the ssl key file/cert file and a self-signed one will be generated for you
at startup. Note that you'll need to provide the `--ssl-skip-verify` option on the client
for this to work.

## Contribute

- Fork repository
- Create a feature or bugfix branch
- Open a new pull request

## License

The MIT License (MIT)

Copyright (c) 2015 Daniil Kulchenko <daniil@kulchenko.com>
