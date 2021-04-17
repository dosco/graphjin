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
BUILD_FLAGS ?= -ldflags '-s -w -X "serv.version=${BUILD_VERSION}" -X "serv.commit=${BUILD}" -X "serv.date=${BUILD_DATE}"'

.PHONY: all download-tools build gen clean test run lint changlog release version help $(PLATFORMS)

test:
	@go test -v -short -race ./...

BIN_DIR := $(GOPATH)/bin
WEB_BUILD_DIR := ./internal/serv/web/build/manifest.json

download-tools:
	@echo Installing tools from tools.go
	@cat tools.go | grep _ | awk -F'"' '{print $$2}' | xargs -tI % go install %

$(WEB_BUILD_DIR):
	@echo "First install Yarn and create a build of the web UI then re-run make install"
	@echo "Run this command: yarn --cwd internal/serv/web/ build"
	@exit 1

changelog: download-tools
	@git-chglog $(ARGS)

lint: download-tools
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

gen: download-tools
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