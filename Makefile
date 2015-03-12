.PHONY: build

all: build

bootstrap:
	@gox -build-toolchain

setup:
	@mkdir build || true
	go get github.com/tools/godep
	@cp bindata.go.tmpl bindata.go
	godep get github.com/mitchellh/gox
	godep get github.com/jteeuwen/go-bindata/...
	godep restore

build:
	@go build .

run:
	@./watchdb

clean:
	@rm watchdb bindata.go >/dev/null 2>&1 || true
	@cp bindata.go.tmpl bindata.go

fullclean: clean
	@rm -f build/*

create-zip:
	@mv watchdb_$(build_os)$(dest_ext) build/watchdb$(dest_ext)
	@cd build && zip watchdb_$(build_os).zip watchdb$(dest_ext) && rm watchdb$(dest_ext)

build-linux: clean
	@go-bindata -prefix sqlite-bin/linux/ sqlite-bin/linux/
	@gox -osarch="linux/386"
	@gox -osarch="linux/amd64"
	@$(MAKE) create-zip build_os=linux_386
	@$(MAKE) create-zip build_os=linux_amd64

build-osx: clean
	@go-bindata -prefix sqlite-bin/osx/ sqlite-bin/osx/
	@gox -os="darwin"
	@$(MAKE) create-zip build_os=darwin_386
	@$(MAKE) create-zip build_os=darwin_amd64

build-windows: clean
	@go-bindata -prefix sqlite-bin/windows/ sqlite-bin/windows/
	@gox -os="windows"
	@$(MAKE) create-zip build_os=windows_386 dest_ext=.exe
	@$(MAKE) create-zip build_os=windows_amd64 dest_ext=.exe

build-all: build-linux build-windows build-osx clean
