module github.com/dosco/graphjin

require (
	cloud.google.com/go/monitoring v1.4.0 // indirect
	cloud.google.com/go/trace v1.2.0 // indirect
	contrib.go.opencensus.io/exporter/aws v0.0.0-20200617204711-c478e41e60e9
	contrib.go.opencensus.io/exporter/prometheus v0.4.1
	contrib.go.opencensus.io/exporter/stackdriver v0.13.10
	contrib.go.opencensus.io/exporter/zipkin v0.1.2
	contrib.go.opencensus.io/integrations/ocsql v0.1.7
	filippo.io/age v1.0.0 // indirect
	github.com/Azure/azure-sdk-for-go v63.1.0+incompatible // indirect
	github.com/Azure/go-autorest/autorest v0.11.25 // indirect
	github.com/Azure/go-autorest/autorest/azure/auth v0.5.11 // indirect
	github.com/Azure/go-autorest/autorest/to v0.4.0 // indirect
	github.com/Azure/go-autorest/autorest/validation v0.3.1 // indirect
	github.com/adjust/gorails v0.0.0-20171013043634-2786ed0c03d3
	github.com/avast/retry-go v3.0.0+incompatible
	github.com/aws/aws-sdk-go v1.43.33 // indirect
	github.com/bradfitz/gomemcache v0.0.0-20220106215444-fb4bf637b56d
	github.com/brianvoe/gofakeit/v6 v6.15.0
	github.com/cenkalti/backoff/v3 v3.2.2 // indirect
	github.com/chirino/graphql v0.0.0-20210707003802-dfaf250c773e
	github.com/containerd/containerd v1.5.9 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/docker/distribution v2.8.1+incompatible // indirect
	github.com/dop251/goja v0.0.0-20220408131256-ffe77e20c6f1
	github.com/frankban/quicktest v1.14.2 // indirect
	github.com/fsnotify/fsnotify v1.5.1
	github.com/go-chi/chi v1.5.4 // indirect
	github.com/go-http-utils/headers v0.0.0-20181008091004-fed159eddc2a
	github.com/go-pkgz/expirable-cache v0.0.3
	github.com/go-playground/validator/v10 v10.10.1
	github.com/go-resty/resty/v2 v2.7.0
	github.com/go-sql-driver/mysql v1.6.0
	github.com/go-test/deep v1.0.4 // indirect
	github.com/gobuffalo/flect v0.2.5
	github.com/golang-jwt/jwt v3.2.2+incompatible
	github.com/golang-jwt/jwt/v4 v4.4.1 // indirect
	github.com/gomodule/redigo v1.8.8
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/gorilla/websocket v1.5.0
	github.com/gosimple/slug v1.12.0
	github.com/hashicorp/go-hclog v1.2.0 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.0 // indirect
	github.com/hashicorp/go-secure-stdlib/mlock v0.1.2 // indirect
	github.com/hashicorp/go-secure-stdlib/parseutil v0.1.4 // indirect
	github.com/hashicorp/go-secure-stdlib/strutil v0.1.2 // indirect
	github.com/hashicorp/go-version v1.4.0 // indirect
	github.com/hashicorp/golang-lru v0.5.5-0.20210104140557-80c98217689d
	github.com/hashicorp/vault/api v1.5.0 // indirect
	github.com/hashicorp/yamux v0.0.0-20211028200310-0bc27b27de87 // indirect
	github.com/howeyc/gopass v0.0.0-20210920133722-c8aef6fb66ef // indirect
	github.com/jackc/pgx/v4 v4.15.0
	github.com/jhump/protoreflect v1.6.1 // indirect
	github.com/jvatic/goja-babel v0.0.0-20220412122858-f2bd58c5ff3f
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0
	github.com/klauspost/compress v1.15.1
	github.com/lestrrat-go/blackmagic v1.0.1 // indirect
	github.com/lestrrat-go/httpcc v1.0.1 // indirect
	github.com/lestrrat-go/iter v1.0.2 // indirect
	github.com/lestrrat-go/jwx v1.2.21
	github.com/magiconair/properties v1.8.6 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/go-testing-interface v1.14.1 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/mitchellh/hashstructure/v2 v2.0.2
	github.com/mitchellh/mapstructure v1.4.3
	github.com/oklog/run v1.1.0 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/openzipkin/zipkin-go v0.4.0
	github.com/orlangure/gnomock v0.19.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/common v0.33.0 // indirect
	github.com/prometheus/statsd_exporter v0.22.4 // indirect
	github.com/rogpeppe/go-internal v1.8.1 // indirect
	github.com/rs/cors v1.8.2
	github.com/rs/xid v1.4.0
	github.com/shopspring/decimal v1.3.1 // indirect
	github.com/spf13/afero v1.8.2
	github.com/spf13/cobra v1.4.0
	github.com/spf13/viper v1.10.1
	github.com/stretchr/objx v0.3.0 // indirect
	github.com/stretchr/testify v1.7.1
	github.com/tj/assert v0.0.3
	go.mozilla.org/sops/v3 v3.7.2
	go.opencensus.io v0.23.0
	go.uber.org/multierr v1.8.0 // indirect
	go.uber.org/zap v1.21.0
	golang.org/x/crypto v0.0.0-20220331220935-ae2d96664a29
	golang.org/x/net v0.0.0-20220403103023-749bd193bc2b // indirect
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/sys v0.0.0-20220405210540-1e041c57c461 // indirect
	golang.org/x/text v0.3.7
	golang.org/x/time v0.0.0-20220224211638-0e9765cccd65
	google.golang.org/api v0.74.0 // indirect
	google.golang.org/genproto v0.0.0-20220405205423-9d709892a2bf // indirect
	gopkg.in/ini.v1 v1.66.4 // indirect
	gopkg.in/square/go-jose.v2 v2.6.0 // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b
)

go 1.16
