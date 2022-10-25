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
BUILD_FLAGS ?= -ldflags '-s -w -X "github.com/dosco/graphjin/serv.version=${BUILD_VERSION}" -X "github.com/dosco/graphjin/serv.commit=${BUILD}" -X "github.com/dosco/graphjin/serv.date=${BUILD_DATE}"'

.PHONY: all download-tools build gen clean tidy test test-norace run lint changlog release version help $(PLATFORMS)

tidy:
	@go mod tidy -go=1.16 && go mod tidy -go=1.17

test:
	@go test -v -timeout 50m -race ./... 
	@go test -v -timeout 50m -race -db=mysql -tags=mysql -run=Example_query ./...
# @go test -v -timeout 50m -race -db=mysql -tags=mysql -run=Example_sub ./...

test-norace:
	@go test -v -timeout 50m ./... 
	@go test -v -timeout 50m -db=mysql -tags=mysql -run=Example_query ./...
# @go test -v -timeout 50m -db=mysql -tags=mysql -run=Example_sub ./...

BIN_DIR := $(GOPATH)/bin
WEB_BUILD_DIR := ./serv/web/build/manifest.json

# @echo Installing tools from tools.go
# @cat tools.go | grep _ | awk -F'"' '{print $$2}' | xargs -tI % go install %
download-tools:
	@go install github.com/git-chglog/git-chglog/cmd/git-chglog@latest
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install golang.org/x/perf/cmd/benchstat@latest
	@go install golang.org/x/tools/cmd/stringer@latest

$(WEB_BUILD_DIR):
	@echo "First install Yarn and create a build of the web UI then re-run make install"
	@echo "Run this command: yarn --cwd serv/web/ build"
	@exit 1

changelog: download-tools
	@git-chglog $(ARGS)

lint: download-tools
	@golangci-lint run ./...

BINARY := graphjin
LDFLAGS := -s -w
PLATFORMS := linux/amd64 windows/amd64 darwin/amd64

temp = $(subst /, ,$@)
os = $(word 1, $(temp))
arch = $(word 2, $(temp))

$(PLATFORMS): lint test 
	@mkdir -p release
	@CGO_ENABLED=0 GOOS=$(os) GOARCH=$(arch) go build $(BUILD_FLAGS) -o release/$(BINARY)-$(BUILD_VERSION)-$(os) main.go

release: linux/amd64 windows/amd64 darwin/amd64

all: lint test $(BINARY)

build: $(BINARY)

gen: download-tools
	@go generate ./...

$(BINARY):
	@CGO_ENABLED=0 go build $(BUILD_FLAGS) -o $(BINARY) main.go 

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