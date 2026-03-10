module github.com/aegion-dynamic/graphjin/wasm/v3

go 1.18

require (
	github.com/aegion-dynamic/graphjin/conf/v3 v3.0.0
	github.com/aegion-dynamic/graphjin/core/v3 v3.0.0
)
replace github.com/aegion-dynamic/graphjin/conf/v3 => ../conf
replace github.com/aegion-dynamic/graphjin/core/v3 => ../core

require (
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	golang.org/x/sync v0.8.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
