module github.com/dosco/graphjin/wasm/v3

go 1.18

require (
	github.com/dosco/graphjin/conf/v3 v3.0.32
	github.com/dosco/graphjin/core/v3 v3.0.32
)

replace github.com/dosco/graphjin/core/v3 => ../core

replace github.com/dosco/graphjin/conf/v3 => ../conf

require (
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	golang.org/x/sync v0.7.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
