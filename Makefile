BINARY := docker-account
VERSION ?= v0.8.2
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test install uninstall

build:
	mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/docker-account

test:
	go test ./...

install: build
	mkdir -p "$(HOME)/.docker/cli-plugins"
	cp bin/$(BINARY) "$(HOME)/.docker/cli-plugins/$(BINARY)"
	chmod 755 "$(HOME)/.docker/cli-plugins/$(BINARY)"

uninstall:
	rm -f "$(HOME)/.docker/cli-plugins/$(BINARY)"
