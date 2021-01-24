#SRC := $(shell find . -type f -name '*.go' -not -name 'bound.go') go.mod
ASSETS :=  $(shell find public -type f -not -name '*.gz')
ASSETS += public/app.wasm
ASSETS += public/index.html public/app.js public/wasm_exec.js public/wasm_init.js public/style.css

TPL := '{{ $$root := .Dir }}{{ range .GoFiles }}{{ printf "%s/%s\n" $$root . }}{{ end }}'
CLIENT_DEPS = $(shell go list -f '{{ join .Deps "\n" }}' ./cmd/client)
CLIENT_FILES = $(shell go list -f $(TPL) ./cmd/client $(CLIENT_DEPS))

SERVER_DEPS = $(shell go list -f '{{ join .Deps "\n" }}' ./cmd/server)
SERVER_FILES = $(shell go list -f $(TPL) ./cmd/server $(SERVER_DEPS))

WASM_DEPS = $(shell GOOS=js GOARCH=wasm go list -f '{{ join .Deps "\n" }}' ./cmd/wasm)
WASM_FILES = $(shell GOOS=js GOARCH=wasm go list -f $(TPL) ./cmd/wasm $(WASM_DEPS))

BINDATA_DEPS = $(shell go list -f '{{ join .Deps "\n" }}' ./cmd/bindata)
BINDATA_FILES = $(shell go list -f $(TPL) ./cmd/bindata $(BINDATA_DEPS))

ASSETS_GZ := $(foreach f, $(ASSETS), $(f).gz)
CLIENTS := dist/homechat-linux-amd64 dist/homechat-darwin-amd64 dist/homechat-linux-arm64 dist/homechat-windows-amd64.exe

PCLIENTS:= $(patsubst dist/%, public/clients/%, $(CLIENTS))
PCLIENTS_GZ := $(foreach f, $(PCLIENTS), $(f).gz)
NATIVE := dist/homechat-$(shell go env GOOS)-$(shell go env GOARCH)

TESTCLIENT := $(patsubst testclient.def/%, testclient/%, $(wildcard testclient.def/*))

VERSION=$(shell git describe)
LDFLAGS=-s -w -X github.com/frizinak/homechat/vars.GitVersion=$(VERSION)

.PHONY: all
all: clients server

.PHONY: server
server: dist/homechat-server

.PHONY: client
client: $(NATIVE)

.PHONY: clients
clients: $(CLIENTS)

.PHONY: install
install: $(NATIVE) dist/homechat-server
	cp -f dist/homechat-server "$$GOBIN/homechat-server"
	cp -f "$(NATIVE)" "$$GOBIN/homechat"

dist/homechat-server: $(SERVER_FILES) bound/bound.go | dist
	go build -o "$@" ./cmd/server

public/app.wasm: $(WASM_FILES)
	GOARCH=wasm GOOS=js go build -o $@ -trimpath -ldflags "$(LDFLAGS)" ./cmd/wasm

public/%.gz: public/%
	cat "$<" | gzip > "$@"

dist/homechat-%: $(CLIENT_FILES) | dist
	GOOS=$$(echo $* | cut -d- -f1) \
		 GOARCH=$$(echo $* | cut -d- -f2 | cut -d. -f1) \
		 go build -o "$@" -trimpath -ldflags "$(LDFLAGS)" ./cmd/client

public/clients/homechat-%: dist/homechat-% | public/clients
	cp "$<" "$@"

public/clients/homechat-%.gz: public/clients/homechat-% | public/clients
	cat "$<" | gzip > "$@"

dist/bindata: $(BINDATA_FILES)
	go build -o "$@" ./cmd/bindata

bound/bound.go: dist/bindata $(ASSETS) $(ASSETS_GZ) $(PCLIENTS) $(PCLIENTS_GZ)
	./dist/bindata

dist:
	@- mkdir "$@" 2>/dev/null
public/clients:
	@- mkdir "$@" 2>/dev/null

.PHONY: clean
clean:
	rm -rf dist
	rm -rf public/clients
	rm -f public/app.wasm
	rm -f bound/bound.go
	find public -type f -name '*.gz' -exec rm {} \;

testclient/%: testclient.def/%
	@-mkdir testclient &>/dev/null
	cp "$<" "$@"

.PHONY: serve
serve: $(SERVER_FILES) bound/bound.go testclient/server.json
	go run ./cmd/server -c ./testclient/server.json

.PHONY: serve-live
serve-live: $(SERVER_FILES) bound/bound.go testclient/server.json
	go run ./cmd/server -c ./testclient/server.json serve -http ./public

.PHONY: local
local: $(NATIVE) $(TESTCLIENT)
	$(NATIVE) -c ./testclient

.PHONY: local-music
local-music: $(NATIVE) $(TESTCLIENT)
	$(NATIVE) -c ./testclient music remote

