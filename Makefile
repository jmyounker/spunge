all: clean update build test

clean:
	rm -f spunge

update:
	go get
	go get github.com/jyounker/vers

build-vers:
	make -C $$GOPATH/src/github.com/jyounker/vers build

set-version: build-vers
	$(eval VERSION := $(shell $$GOPATH/bin/vers -f version.json show))
	
build: set-version
	go build -ldflags "-X main.version=$(VERSION)"

test: build
	go test

package-base: test
	mkdir target
	mkdir target/model
	mkdir target/package

package-osx: package-base
	mkdir target/model/osx
	mkdir target/model/osx/usr
	mkdir target/model/osx/usr/local
	mkdir target/model/osx/usr/local/bin
	install -m 755 spunge target/model/osx/usr/local/bin/spunge
	fpm -s dir -t osxpkg -n spunge -v $(shell vers -f version.json show) -p target/package -C target/model/osx .

package-rpm: package-base
	mkdir target/model/linux-x86-rpm
	mkdir target/model/linux-x86-rpm/usr
	mkdir target/model/linux-x86-rpm/usr/bin
	install -m 755 spunge target/model/linux-x86-rpm/usr/bin/spunge
	fpm -s dir -t rpm -n spunge -v $(shell vers -f version.json show) -p target/package -C target/model/linux-x86-rpm .

package-deb: package-base
	mkdir target/model/linux-x86-deb
	mkdir target/model/linux-x86-deb/usr
	mkdir target/model/linux-x86-deb/usr/bin
	install -m 755 spunge target/model/linux-x86-deb/usr/bin/spunge
	fpm -s dir -t deb -n spunge -v $(shell vers -f version.json show) -p target/package -C target/model/linux-x86-deb .

