BUILD         ?= $(shell git rev-parse --short HEAD)
BUILD_DATE    ?= $(shell git log -1 --format=%ci)
BUILD_BRANCH  ?= $(shell git rev-parse --abbrev-ref HEAD)
BUILD_VERSION ?= $(shell git describe --always --tags)

GOPATH  ?= $(shell go env GOPATH)

ifndef GOPATH
override GOPATH = $(HOME)/go
endif

export GO111MODULE := on

# Build-time Go variables
BUILD_FLAGS ?= -ldflags '-s -w -X "main.version=${BUILD_VERSION}" -X "main.commit=${BUILD}" -X "main.date=${BUILD_DATE}"'

.PHONY: all build gen clean test run lint changlog release version help $(PLATFORMS)

test:
	@go test -v -short -race ./...

BIN_DIR := $(GOPATH)/bin
GORICE := $(BIN_DIR)/rice
GOSTRINGER := $(BIN_DIR)/stringer
GOLANGCILINT := $(BIN_DIR)/golangci-lint
GITCHGLOG := $(BIN_DIR)/git-chglog
WEB_BUILD_DIR := ./internal/serv/web/build/manifest.json

$(GORICE):
	@GO111MODULE=off go get -u github.com/GeertJohan/go.rice/rice

$(GOSTRINGER):
	@GO111MODULE=off go get -u golang.org/x/tools/cmd/stringer

$(WEB_BUILD_DIR):
	@echo "First install Yarn and create a build of the web UI then re-run make install"
	@echo "Run this command: yarn --cwd internal/serv/web/ build"
	@exit 1

$(GITCHGLOG):
	@GO111MODULE=off go get -u github.com/git-chglog/git-chglog/cmd/git-chglog

changelog: $(GITCHGLOG)
	@git-chglog $(ARGS)

$(GOLANGCILINT):
	@GO111MODULE=off curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh| sh -s -- -b $(GOPATH)/bin v1.37.0

lint: $(GOLANGCILINT)
	@golangci-lint run ./... --skip-dirs-use-default

BINARY := graphjin
LDFLAGS := -s -w
PLATFORMS := windows linux darwin
os = $(word 1, $@)

$(PLATFORMS): lint test 
	@mkdir -p release
	@GOOS=$(os) GOARCH=amd64 go build $(BUILD_FLAGS) -o release/$(BINARY)-$(BUILD_VERSION)-$(os)-amd64 main.go

release: windows linux darwin

all: lint test $(BINARY)

build: $(BINARY)

gen: $(GOSTRINGER) $(GORICE) $(WEB_BUILD_DIR)
	@go generate ./...

$(BINARY): clean
	@go build $(BUILD_FLAGS) -o $(BINARY) main.go 

clean:
	@rm -f $(BINARY)

run: clean
	@go run $(BUILD_FLAGS) main.go $(ARGS)

install: clean build
	@echo "Commit Hash: `git rev-parse HEAD`"
	@echo "Old Hash: `shasum $(GOPATH)/bin/$(BINARY) 2>/dev/null | cut -c -32`"
	@mv $(BINARY) $(GOPATH)/bin/$(BINARY)
	@echo "New Hash:" `shasum $(GOPATH)/bin/$(BINARY) 2>/dev/null | cut -c -32`

uninstall: clean
	@go clean -i -x

version:
	@echo GraphJin ${BUILD_VERSION}
	@echo Build: ${BUILD}
	@echo Build date: ${BUILD_DATE}
	@echo Branch: ${BUILD_BRANCH}
	@echo Go version: $(shell go version)

help:
	@echo
	@echo Build commands:
	@echo " make build         - Build graphjin binary"
	@echo " make install       - Install graphjin binary"
	@echo " make uninstall     - Uninstall graphjin binary"
	@echo " make [platform]    - Build for platform [linux|darwin|windows]"
	@echo " make release       - Build all platforms"
	@echo " make run           - Run graphjin (eg. make run ARGS=\"help\")"
	@echo " make test          - Run all tests"
	@echo " make changelog     - Generate changelog (eg. make changelog ARGS=\"help\")"
	@echo " make help          - This help"
	@echo