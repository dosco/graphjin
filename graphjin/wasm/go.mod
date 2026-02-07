module github.com/dosco/graphjin/wasm/v3

go 1.24.0

require (
	github.com/dosco/graphjin/conf/v3 v3.3.2
	github.com/dosco/graphjin/core/v3 v3.3.2
)

replace (
	github.com/dosco/graphjin/conf/v3 => ../conf
	github.com/dosco/graphjin/core/v3 => ../core
)

require (
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/kr/text v0.2.0 // indirect
	golang.org/x/sync v0.17.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
