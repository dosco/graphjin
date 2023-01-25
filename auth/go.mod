module github.com/dosco/graphjin/auth/v3

go 1.18

replace github.com/dosco/graphjin/core/v3 => ../core

replace github.com/dosco/graphjin/plugin/osfs/v3 => ../plugin/osfs

require (
	github.com/adjust/gorails v0.0.0-20171013043634-2786ed0c03d3
	github.com/bradfitz/gomemcache v0.0.0-20230124162541-5f7a7d875746
	github.com/dosco/graphjin/core/v3 v3.0.0-00010101000000-000000000000
	github.com/dosco/graphjin/plugin/osfs/v3 v3.0.0-00010101000000-000000000000
	github.com/golang-jwt/jwt v3.2.2+incompatible
	github.com/gomodule/redigo v1.8.9
	github.com/gorilla/websocket v1.5.0
	github.com/lestrrat-go/jwx v1.2.25
	github.com/stretchr/testify v1.8.1
	go.uber.org/zap v1.24.0
	golang.org/x/crypto v0.5.0
)

require (
	github.com/avast/retry-go v3.0.0+incompatible // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.1.0 // indirect
	github.com/goccy/go-json v0.10.0 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/kr/pretty v0.3.0 // indirect
	github.com/lestrrat-go/backoff/v2 v2.0.8 // indirect
	github.com/lestrrat-go/blackmagic v1.0.1 // indirect
	github.com/lestrrat-go/httpcc v1.0.1 // indirect
	github.com/lestrrat-go/iter v1.0.2 // indirect
	github.com/lestrrat-go/option v1.0.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/go-internal v1.8.1 // indirect
	github.com/rs/xid v1.4.0 // indirect
	go.uber.org/atomic v1.10.0 // indirect
	go.uber.org/goleak v1.1.12 // indirect
	go.uber.org/multierr v1.9.0 // indirect
	golang.org/x/sync v0.1.0 // indirect
	golang.org/x/text v0.6.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
