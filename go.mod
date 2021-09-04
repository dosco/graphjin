module github.com/dosco/graphjin

replace github.com/gobuffalo/flect => github.com/renathoaz/flect v0.2.3-0.20200901003717-8573c32cc9d7

require (
	contrib.go.opencensus.io/exporter/aws v0.0.0-20200617204711-c478e41e60e9
	contrib.go.opencensus.io/exporter/prometheus v0.2.0
	contrib.go.opencensus.io/exporter/stackdriver v0.13.4
	contrib.go.opencensus.io/exporter/zipkin v0.1.2
	contrib.go.opencensus.io/integrations/ocsql v0.1.7
	github.com/NYTimes/gziphandler v1.1.1
	github.com/adjust/gorails v0.0.0-20171013043634-2786ed0c03d3
	github.com/bradfitz/gomemcache v0.0.0-20190913173617-a41fca850d0b
	github.com/brianvoe/gofakeit/v6 v6.0.0
	github.com/chirino/graphql v0.0.0-20200723175208-cec7bf430a98
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/dop251/goja v0.0.0-20210322220816-6fc852574a34
	github.com/fsnotify/fsnotify v1.4.9
	github.com/garyburd/redigo v1.6.2
	github.com/git-chglog/git-chglog v0.10.0
	github.com/go-http-utils/headers v0.0.0-20181008091004-fed159eddc2a
	github.com/go-pkgz/expirable-cache v0.0.3
	github.com/go-sql-driver/mysql v1.6.0
	github.com/gobuffalo/flect v0.2.2
	github.com/golangci/golangci-lint v1.37.0
	github.com/gopherjs/gopherjs v0.0.0-20190430165422-3e4dfb77656c // indirect
	github.com/goreleaser/goreleaser v0.157.0
	github.com/gorilla/websocket v1.4.2
	github.com/gosimple/slug v1.9.0
	github.com/hashicorp/golang-lru v0.5.4
	github.com/jackc/pgproto3/v2 v2.0.4 // indirect
	github.com/jackc/pgx/v4 v4.8.1
	github.com/jvatic/goja-babel v0.0.0-20210512011721-9ad52e4ef752
	github.com/klauspost/compress v1.13.1 // indirect
	github.com/lestrrat-go/jwx v1.1.3
	github.com/magiclabs/magic-admin-go v0.1.0
	github.com/mitchellh/mapstructure v1.4.1
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/openzipkin/zipkin-go v0.2.4
	github.com/orlangure/gnomock v0.14.2
	github.com/pkg/errors v0.9.1
	github.com/prometheus/common v0.14.0 // indirect
	github.com/prometheus/procfs v0.2.0 // indirect
	github.com/prometheus/statsd_exporter v0.18.0 // indirect
	github.com/rs/cors v1.7.0
	github.com/rs/xid v1.3.0
	github.com/spf13/cobra v1.2.1
	github.com/spf13/viper v1.8.1
	github.com/stretchr/testify v1.7.0
	github.com/tj/assert v0.0.3
	go.opencensus.io v0.23.0
	go.uber.org/zap v1.17.0
	golang.org/x/crypto v0.0.0-20210322153248-0c34fe9e7dc2
	golang.org/x/lint v0.0.0-20210508222113-6edffad5e616
	golang.org/x/perf v0.0.0-20201207232921-bdcc6220ee90
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/sys v0.0.0-20210630005230-0f9fa26af87c // indirect
	golang.org/x/text v0.3.6 // indirect
	golang.org/x/time v0.0.0-20210220033141-f8bda1e9f3ba
	golang.org/x/tools v0.1.2
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b
)

go 1.16
