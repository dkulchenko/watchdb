# watchdb

A tool for easily replicating a SQLite database across a network.

## Overview

watchdb is a tool that enables quick setup of master-slave synchronization for 
SQLite databases across a network.

Synchronization is one-way only (changes made on the master will overwrite
changes made on slaves). Slave databases are kept read-only to prevent 
accidental writes from application code.

watchdb replication is eventually consistent by design, so it's AP in [CAP](http://en.wikipedia.org/wiki/CAP_theorem).
If you need strong consistency (at the expense of performance and required changes to 
application code), take a look at [rqlite](https://github.com/otoolep/rqlite).

## Features

- Works on Linux, OS X, Windows
- No dependencies, just one binary
- Replicates sqlite DB changes quickly across any number of nodes
- No changes to application code required
- Optional authentication and encryption supported

In the future:

- Incremental (partial) DB syncing

## Installation

[Precompiled binaries](https://github.com/dkulchenko/watchdb/releases) for supported 
operating systems are available.

Alternatively, run `go get github.com/dkulchenko/watchdb`. You'll need a working sqlite3 
binary in $PATH if you go this route. The precompiled binaries embed a copy of sqlite.

## Usage

Watch a SQLite database:

```
watchdb watch mydb.sqlite
```

On a slave server, sync:

```
watchdb sync 127.0.0.1:8144 mydbcopy.sqlite
```

Easy as that. Any changes made to mydb.sqlite will quickly show up in mydbcopy.sqlite. Try it out!

### Options

You can specify any option on the command line, or provide a configuration file (an example config is available at config/example.yml):

```
watchdb watch --config-file=config.yml mydb.sqlite
```

Set a custom bind address/port:

```
watchdb watch --bind-addr=127.0.0.1 --bind-port=1234 mydb.sqlite
```

### Authentication

Require an auth key to be sent before syncing is allowed (similar to Redis AUTH):

```
watchdb watch --auth-key 927bc430fc2195fa2f0caaf35d115c mydb.sqlite
```

and on the slave:

```
watchdb sync --auth-key 927bc430fc2195fa2f0caaf35d115c 127.0.0.1:8144 mydbcopy.sqlite
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

## Why?

sqlite3 is an excellent database, and by far the easiest way to embed SQL into an app
where using a larger DB like MySQL or PostgreSQL might not be possible. The downside, 
of course, is the single-node nature of the DB, which means that if the primary goes 
down, the data goes down with it.

I wanted to build a simple way to replicate a sqlite database across a network without
requiring any special configuration or changes to application code. Hence, watchdb.

## Contribute

- Fork repository
- Create a feature or bugfix branch
- Open a new pull request

## License

The MIT License (MIT)

Copyright (c) 2015 Daniil Kulchenko <daniil@kulchenko.com>
