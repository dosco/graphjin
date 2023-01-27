module github.com/dosco/graphjin/plugin/otel/v3

go 1.19

replace github.com/dosco/graphjin/core/v3 => ../../core

require (
	github.com/dosco/graphjin/core/v3 v3.0.0-00010101000000-000000000000
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.37.0
	go.opentelemetry.io/otel v1.11.2
	go.opentelemetry.io/otel/trace v1.11.2
)

require (
	github.com/felixge/httpsnoop v1.0.3 // indirect
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	go.opentelemetry.io/otel/metric v0.34.0 // indirect
	golang.org/x/sync v0.1.0 // indirect
)
