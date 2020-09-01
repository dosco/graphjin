---
id: config
title: Configuration
sidebar_label: Configuration
---

Configuration files can either be in YAML or JSON; their names are derived from the `GO_ENV` variable, for example `GO_ENV=prod` will cause the `prod.yaml` config file to be used, `GO_ENV=dev` will use the `dev.yaml`. A path to look for the config files can be specified using the `-path <folder>` command line argument.

We've tried to ensure that the config file is self-documenting and easy to work with.

```yaml
# Inherit config from this other config file
# so I only need to overwrite some values
inherits: base

app_name: "Super Graph Development"
host_port: 0.0.0.0:8080
web_ui: true

# debug, error, warn, info
log_level: "debug"

# enable or disable http compression (uses gzip)
http_compress: true

# When production mode is 'true' only queries
# from the allow list are permitted.
# When it's 'false' all queries are saved to the
# the allow list in ./config/allow.list
production: false

# Throw a 401 on auth failure for queries that need auth
auth_fail_block: false

# Latency tracing for database queries and remote joins
# the resulting latency information is returned with the
# response
enable_tracing: true

# Watch the config folder and reload Super Graph
# with the new configs when a change is detected
reload_on_config_change: true

# File that points to the database seeding script
# seed_file: seed.js

# Path pointing to where the migrations can be found
migrations_path: ./migrations

# Set session variable "user.id" to the user id
# Enable this if you need the user id in triggers, etc
# Note: This will not work with subscriptions
set_user_id: false

# inflections:
#   person: people
#   sheep: sheep

# open opencensus tracing and metrics
telemetry:
  debug: true
  metrics:
    exporter: "prometheus"
  tracing:
    exporter: "zipkin"
    endpoint: "http://zipkin:9411/api/v2/spans"
    sample: 0.6

# Rate is the number of events per second
# Bucket a burst of at most 'bucket' number of events.
# ip_header sets the header that contains the client ip.
# https://en.wikipedia.org/wiki/Token_bucket
rate_limiter:
  rate: 2
  bucket: 3
  ip_header: X-Forwarded-For

# Enable additional debugging logs
debug: false

# Auth related environment Variables
# SG_AUTH_RAILS_COOKIE_SECRET_KEY_BASE
# SG_AUTH_RAILS_REDIS_URL
# SG_AUTH_RAILS_REDIS_PASSWORD
# SG_AUTH_JWT_PUBLIC_KEY_FILE

auth:
  # Can be 'rails' or 'jwt'
  type: rails
  cookie: _app_session

  # Comment this out if you want to disable setting
  # the user_id via a header for testing.
  # Disable in production
  creds_in_header: true

  rails:
    # Rails version this is used for reading the
    # various cookies formats.
    version: 5.2

    # Found in 'Rails.application.config.secret_key_base'
    secret_key_base: 0a248500a64c01184edb4d7ad3a805488f8097ac761b76aaa6c17c01dcb7af03a2f18b

    # Remote cookie store. (memcache or redis)
    # url: redis://redis:6379
    # password: ""
    # max_idle: 80
    # max_active: 12000
    # In most cases you don't need these
    # salt: "encrypted cookie"
    # sign_salt: "signed encrypted cookie"
    # auth_salt: "authenticated encrypted cookie"

  # jwt:
  #   provider: auth0
  #   secret: abc335bfcfdb04e50db5bb0a4d67ab9
  #   public_key_file: /secrets/public_key.pem
  #   public_key_type: ecdsa #rsa
  # header:
  #   name: dnt
  #   exists: true
  #   value: localhost:8080

# You can add additional named auths to use with actions
# In this example actions using this auth can only be
# called from the Google Appengine Cron service that
# sets a special header to all it's requests
auths:
  - name: from_taskqueue
    type: header
    header:
      name: X-Appengine-Cron
      exists: true

# Postgres related environment Variables
# SG_DATABASE_HOST
# SG_DATABASE_PORT
# SG_DATABASE_USER
# SG_DATABASE_PASSWORD

database:
  type: postgres
  host: db
  port: 5432
  dbname: app_development
  user: postgres
  password: postgres

  #schema: "public"
  #pool_size: 10
  #max_retries: 0
  #log_level: "debug"

  # database ping timeout is used for db health checking
  ping_timeout: 1m

  # Set up an secure tls encrypted db connection
  enable_tls: false

  # Required for tls. For example with Google Cloud SQL it's
  # <gcp-project-id>:<cloud-sql-instance>"
  # server_name: blah
  # Required for tls. Can be a file path or the contents of the pem file
  # server_cert: ./server-ca.pem
  # Required for tls. Can be a file path or the contents of the pem file
  # client_cert: ./client-cert.pem
  # Required for tls. Can be a file path or the contents of the pem file
  # client_key: ./client-key.pem

# Define additional variables here to be used with filters
# Variables used require a type suffix eg. $user_id:bigint
variables:
  admin_account_id: "5"
  # admin_account_id: "sql:select id from users where admin = true limit 1

# Field and table names that you wish to block
blocklist:
  - ar_internal_metadata
  - schema_migrations
  - secret
  - password
  - encrypted
  - token

# Create custom actions with their own api endpoints
# For example the below action will be available at /api/v1/actions/refresh_leaderboard_users
# A request to this url will execute the configured SQL query
# which in this case refreshes a materialized view in the database.
# The auth_name is from one of the configured auths
actions:
  - name: refresh_leaderboard_users
    sql: REFRESH MATERIALIZED VIEW CONCURRENTLY "leaderboard_users"
    auth_name: from_taskqueue

tables:
  - name: customers
    remotes:
      - name: payments
        id: stripe_id
        url: http://rails_app:3000/stripe/$id
        path: data
        # debug: true
        pass_headers:
          - cookie
        set_headers:
          - name: Host
            value: 0.0.0.0
          # - name: Authorization
          #   value: Bearer <stripe_api_key>

  - # You can create new fields that have a
    # real db table backing them
    name: me
    table: users

# Variables used require a type suffix eg. $user_id:bigint
roles_query: "SELECT * FROM users WHERE id = $user_id:bigint"

roles:
  - name: anon
    tables:
      - name: products
        limit: 10

        query:
          columns: ["id", "name", "description"]
          aggregation: false

        insert:
          allow: false

        update:
          allow: false

        delete:
          allow: false

  - name: user
    tables:
      - name: users
        query:
          filters: ["{ id: { _eq: $user_id } }"]

      - name: products
        query:
          limit: 50
          filters: ["{ user_id: { eq: $user_id } }"]
          columns: ["id", "name", "description"]
          # This is a role table level config that blocks aggregation functions
          # like `count_id` or custom postgres functions that you can use in your query
          # (https://supergraph.dev/docs/graphql/#custom-functions)
          disable_functions: false

        insert:
          filters: ["{ user_id: { eq: $user_id } }"]
          columns: ["id", "name", "description"]
          set:
            - created_at: "now"

        update:
          filters: ["{ user_id: { eq: $user_id } }"]
          columns:
            - id
            - name
          set:
            - updated_at: "now"

        delete:
          block: true

  - name: admin
    match: id = 1000
    tables:
      - name: users
        filters: []
```

If deploying into environments like Kubernetes it's useful to be able to configure things like secrets and hosts through environment variables therefore we expose the below environment variables. This is especially useful for secrets since they are usually injected in via a secrets management framework (ie. Kubernetes Secrets).

Keep in mind any value can be overwritten using environment variables, for example `auth.jwt.public_key_type` converts to `SG_AUTH_JWT_PUBLIC_KEY_TYPE`. In short prefix `SG_`, uppercase and all `.` should changed to `_`.

#### Postgres environment variables

```bash
SG_DATABASE_HOST
SG_DATABASE_PORT
SG_DATABASE_USER
SG_DATABASE_PASSWORD
```

#### Auth environment variables

```bash
SG_AUTH_RAILS_COOKIE_SECRET_KEY_BASE
SG_AUTH_RAILS_REDIS_URL
SG_AUTH_RAILS_REDIS_PASSWORD
SG_AUTH_JWT_PUBLIC_KEY_FILE
```

## YugabyteDB

Yugabyte is an open-source, geo-distrubuted cloud-native relational DB that scales horizontally. Super Graph works with Yugabyte right out of the box. If you think you're data needs will outgrow Postgres and you don't really want to deal with sharding then Yugabyte is the way to go. Just point Super Graph to your Yugabyte DB and everything will just work including running migrations, seeding, querying, mutations, etc.

To use Yugabyte in your local development flow just uncomment the following lines in the `docker-compose.yml` file that is part of your Super Graph app. Also remember to comment out the originl postgres `db` config.

```yaml
  # Postgres DB
  # db:
  #   image: postgres:latest
  #   ports:
  #     - "5432:5432"

  #Standard config to run a single node of Yugabyte
  yb-master:
    image: yugabytedb/yugabyte:latest
    container_name: yb-master-n1
    command: [ "/home/yugabyte/bin/yb-master",
              "--fs_data_dirs=/mnt/disk0,/mnt/disk1",
              "--master_addresses=yb-master-n1:7100",
              "--replication_factor=1",
              "--enable_ysql=true"]
    ports:
      - "7000:7000"
    environment:
      SERVICE_7000_NAME: yb-master

  db:
    image: yugabytedb/yugabyte:latest
    container_name: yb-tserver-n1
    command: [ "/home/yugabyte/bin/yb-tserver",
              "--fs_data_dirs=/mnt/disk0,/mnt/disk1",
              "--start_pgsql_proxy",
              "--tserver_master_addrs=yb-master-n1:7100"]
    ports:
      - "9042:9042"
      - "6379:6379"
      - "5433:5433"
      - "9000:9000"
    environment:
      SERVICE_5433_NAME: ysql
      SERVICE_9042_NAME: ycql
      SERVICE_6379_NAME: yedis
      SERVICE_9000_NAME: yb-tserver
    depends_on:
      - yb-master

  # Environment variables to point Super Graph to Yugabyte
  # This is required since it uses a different user and port number
  yourapp_api:
    image: dosco/super-graph:latest
    environment:
      GO_ENV: "development"
      Uncomment below for Yugabyte DB
      SG_DATABASE_PORT: 5433
      SG_DATABASE_USER: yugabyte
      SG_DATABASE_PASSWORD: yugabyte
    volumes:
     - ./config:/config
    ports:
      - "8080:8080"
    depends_on:
      - db
```
