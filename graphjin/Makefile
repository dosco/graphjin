BUILD         ?= $(shell git rev-parse --short HEAD)
BUILD_DATE    ?= $(shell git log -1 --format=%ci)
BUILD_BRANCH  ?= $(shell git rev-parse --abbrev-ref HEAD)
BUILD_VERSION ?= $(shell git describe --always --tags)

PKGS    := $(shell go list ./... | grep -v /vendor)
GOPATH  ?= $(shell go env GOPATH)

ifndef GOPATH
override GOPATH = $(HOME)/go
endif

export GO111MODULE := on

# Build-time Go variables
version        = github.com/dosco/super-graph/serv.version
gitBranch      = github.com/dosco/super-graph/serv.gitBranch
lastCommitSHA  = github.com/dosco/super-graph/serv.lastCommitSHA
lastCommitTime = github.com/dosco/super-graph/serv.lastCommitTime

BUILD_FLAGS ?= -ldflags '-s -w -X ${lastCommitSHA}=${BUILD} -X "${lastCommitTime}=${BUILD_DATE}" -X "${version}=${BUILD_VERSION}" -X ${gitBranch}=${BUILD_BRANCH}'

.PHONY: all build gen clean test run lint release version help $(PLATFORMS) $(BINARY)

test: lint
	@go test -v $(PKGS)

BIN_DIR := $(GOPATH)/bin
GOLANGCILINT := $(BIN_DIR)/golangci-lint

$(GOLANGCILINT):
	@curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh| sh -s -- -b $(GOPATH)/bin v1.21.0

lint: $(GOMETALINTER)
	@golangci-lint run ./... --skip-dirs-use-default

BINARY := super-graph
LDFLAGS := -s -w
PLATFORMS := windows linux darwin
os = $(word 1, $@)

$(PLATFORMS): gen
	@mkdir -p release
	@GOOS=$(os) GOARCH=amd64 go build $(BUILD_FLAGS) -o release/$(BINARY)-$(BUILD_VERSION)-$(os)-amd64

release: windows linux darwin

all: $(BINARY)

build: $(BINARY)

gen:
	@go install github.com/GeertJohan/go.rice/rice
	@go generate ./...

$(BINARY): clean gen
	@go build $(BUILD_FLAGS) -o $(BINARY)

clean:
	@rm -f $(BINARY)

run: clean
	@go run $(BUILD_FLAGS) main.go $(ARGS)

install: gen
	@echo $(GOPATH)
	@echo "Commit Hash: `git rev-parse HEAD`"
	@echo "Old Hash: `shasum $(GOPATH)/bin/$(BINARY) 2>/dev/null | cut -c -32`"
	@go install $(BUILD_FLAGS)
	@echo "New Hash:" `shasum $(GOPATH)/bin/$(BINARY) 2>/dev/null | cut -c -32`

uninstall: clean
	@go clean -i -x

version:
	@echo Super Graph ${BUILD_VERSION}
	@echo Build: ${BUILD}
	@echo Build date: ${BUILD_DATE}
	@echo Branch: ${BUILD_BRANCH}
	@echo Go version: $(shell go version)

help:
	@echo
	@echo Build commands:
	@echo " make build         - Build supergraph binary"
	@echo " make install       - Install supergraph binary"
	@echo " make uninstall     - Uninstall supergraph binary"
	@echo " make [platform]    - Build for platform [linux|darwin|windows]"
	@echo " make release       - Build all platforms"
	@echo " make run           - Run supergraph (eg. make run ARGS=\"version\")"
	@echo " make version       - Show current build info"
	@echo " make help          - This help"
	@echo