module github.com/dosco/graphjin/conf/v3

go 1.24.0

require (
	github.com/dosco/graphjin/core/v3 v3.2.3
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/dosco/graphjin/core/v3 => ../core

require (
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	golang.org/x/sync v0.17.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)
