module github.com/dosco/graphjin

require (
	contrib.go.opencensus.io/exporter/aws v0.0.0-20200617204711-c478e41e60e9
	contrib.go.opencensus.io/exporter/prometheus v0.4.0
	contrib.go.opencensus.io/exporter/stackdriver v0.13.10
	contrib.go.opencensus.io/exporter/zipkin v0.1.2
	contrib.go.opencensus.io/integrations/ocsql v0.1.7
	github.com/Azure/azure-sdk-for-go v57.0.0+incompatible // indirect
	github.com/Azure/go-autorest/autorest v0.11.20 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.9.15 // indirect
	github.com/Azure/go-autorest/autorest/azure/auth v0.5.8 // indirect
	github.com/Azure/go-autorest/autorest/azure/cli v0.4.3 // indirect
	github.com/Azure/go-autorest/autorest/to v0.4.0 // indirect
	github.com/Azure/go-autorest/autorest/validation v0.3.1 // indirect
	github.com/adjust/gorails v0.0.0-20171013043634-2786ed0c03d3
	github.com/avast/retry-go v3.0.0+incompatible
	github.com/bradfitz/gomemcache v0.0.0-20220106215444-fb4bf637b56d
	github.com/brianvoe/gofakeit/v6 v6.14.3
	github.com/chirino/graphql v0.0.0-20210707003802-dfaf250c773e
	github.com/containerd/containerd v1.5.9 // indirect
	github.com/dop251/goja v0.0.0-20220110113543-261677941f3c
	github.com/fsnotify/fsnotify v1.5.1
	github.com/git-chglog/git-chglog v0.15.1
	github.com/go-http-utils/headers v0.0.0-20181008091004-fed159eddc2a
	github.com/go-pkgz/expirable-cache v0.0.3
	github.com/go-playground/validator/v10 v10.10.0
	github.com/go-resty/resty/v2 v2.7.0
	github.com/go-sql-driver/mysql v1.6.0
	github.com/gobuffalo/flect v0.2.4
	github.com/golang-jwt/jwt v3.2.2+incompatible
	github.com/gomodule/redigo v1.8.8
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/gorilla/websocket v1.4.2
	github.com/gosimple/slug v1.12.0
	github.com/hashicorp/go-retryablehttp v0.7.0 // indirect
	github.com/hashicorp/golang-lru v0.5.5-0.20210104140557-80c98217689d
	github.com/huandu/xstrings v1.3.2 // indirect
	github.com/jackc/pgx/v4 v4.14.1
	github.com/jvatic/goja-babel v0.0.0-20220112112033-3ef795a80dfc
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0
	github.com/klauspost/compress v1.14.1
	github.com/lestrrat-go/jwx v1.2.17
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/hashstructure/v2 v2.0.2
	github.com/mitchellh/mapstructure v1.4.3
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/openzipkin/zipkin-go v0.3.0
	github.com/orlangure/gnomock v0.19.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/common v0.32.1 // indirect
	github.com/prometheus/procfs v0.7.3 // indirect
	github.com/rs/cors v1.8.2
	github.com/rs/xid v1.3.0
	github.com/sergi/go-diff v1.2.0 // indirect
	github.com/spf13/afero v1.8.0
	github.com/spf13/cobra v1.3.0
	github.com/spf13/viper v1.10.1
	github.com/stretchr/testify v1.7.0
	github.com/tj/assert v0.0.3
	go.mozilla.org/sops/v3 v3.7.1
	go.opencensus.io v0.23.0
	go.uber.org/zap v1.20.0
	golang.org/x/crypto v0.0.0-20220112180741-5e0467b6c7ce
	golang.org/x/perf v0.0.0-20211012211434-03971e389cd3
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/time v0.0.0-20211116232009-f0f3c7e86c11
	golang.org/x/tools v0.1.8
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b
)

replace (
	github.com/go-playground/validator/v10 v10.10.0 => ./core/internal/validator
)

go 1.16
