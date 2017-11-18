all: clean update build test show

clean:
	rm -f spunge

update:
	go get

build: build
	go build -ldflags "-X main.version=$(shell vers -f version.json show)"

test: build
	go test
