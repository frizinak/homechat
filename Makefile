SRC := $(shell find . -type f -name '*.go' -not -name 'bound.go') go.mod
ASSETS :=  $(shell find public -type f -not -name '*.gz')
ASSETS += public/app.wasm
ASSETS += public/index.html public/app.js public/wasm_exec.js public/wasm_init.js public/style.css

ASSETS_GZ := $(foreach f, $(ASSETS), $(f).gz)
CLIENTS := dist/homechat-linux-amd64 dist/homechat-darwin-amd64 dist/homechat-linux-arm64 dist/homechat-windows-amd64.exe

PCLIENTS:= $(patsubst dist/%, public/clients/%, $(CLIENTS))
PCLIENTS_GZ := $(foreach f, $(PCLIENTS), $(f).gz)
NATIVE := dist/homechat-$(shell go env GOOS)-$(shell go env GOARCH)

TESTCLIENT := $(patsubst testclient.def/%, testclient/%, $(wildcard testclient.def/*))

.PHONY: all
all: clients server

.PHONY: server
server: dist/homechat-server

.PHONY: client
client: $(NATIVE)

.PHONY: clients
clients: $(CLIENTS)

.PHONY: install
install: $(NATIVE)
	cp -f $< "$$GOBIN/homechat"

dist/homechat-server: $(SRC) bound/bound.go | dist
	go build -o "$@" ./cmd/server

public/app.wasm: $(SRC)
	GOARCH=wasm GOOS=js go build -o $@ -trimpath -ldflags "-s -w" ./cmd/wasm

public/%.gz: public/%
	cat "$<" | gzip > "$@"

dist/homechat-%: $(SRC) | dist
	GOOS=$$(echo $* | cut -d- -f1) \
		 GOARCH=$$(echo $* | cut -d- -f2 | cut -d. -f1) \
		 go build -o "$@" -trimpath -ldflags "-s -w" ./cmd/client

public/clients/homechat-%: dist/homechat-% | public/clients
	cp "$<" "$@"

public/clients/homechat-%.gz: public/clients/homechat-% | public/clients
	cat "$<" | gzip > "$@"

bound/bound.go: $(ASSETS) $(ASSETS_GZ) $(PCLIENTS) $(PCLIENTS_GZ)
	go run ./cmd/bindata

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
serve: $(SRC) bound/bound.go testclient/server.json
	go run ./cmd/server -c ./testclient/server.json

.PHONY: serve-live
serve-live: $(SRC) bound/bound.go testclient/server.json
	go run ./cmd/server -c ./testclient/server.json serve -http ./public

.PHONY: local
local: $(NATIVE) $(TESTCLIENT)
	$(NATIVE) -c ./testclient

.PHONY: local-music
local-music: $(NATIVE) $(TESTCLIENT)
	$(NATIVE) -c ./testclient music

