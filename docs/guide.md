---
sidebar: auto
---

# Guide to Super Graph

Without writing a line of code get an instant high-performance GraphQL API for your Ruby-on-Rails app. Super Graph will automatically understand your apps database and expose a secure, fast and complete GraphQL API for it. Built in support for Rails authentication and JWT tokens.

## Features
- Works with Rails database schemas
- Automatically learns schemas and relationships
- Belongs-To, One-To-Many and Many-To-Many table relationships
- Full text search and Aggregations
- Rails Auth supported (Redis, Memcache, Cookie)
- JWT tokens supported (Auth0, etc)
- Highly optimized and fast Postgres SQL queries
- Configure with a simple config file
- High performance GO codebase
- Tiny docker image and low memory requirements

## Try it out

```bash
# download super graph source
git clone https://github.com/dosco/super-graph.git

# setup the demo rails app & database
./demo setup

# run the demo
./demo run

# signin to the demo app (user1@demo.com / 123456)
open http://localhost:3000

# try the super graph web ui
open http://localhost:8080
```

::: warning DEMO REQUIREMENTS  
This demo requires `docker` you can either install it using `brew` or from the 
docker website https://docs.docker.com/docker-for-mac/install/
:::

#### Trying out GraphQL

We currently support the `query` action which is used for fetching data. Support for `mutation` and `subscriptions` is work in progress. For example the below GraphQL query would fetch two products that belong to the current user where the price is greater than 10

#### GQL Query

```graphql
query { 
  users {
    id
    email
    picture : avatar
    password
    full_name
    products(limit: 2, where: { price: { gt: 10 } }) {
      id
      name
      description
      price
    }
  }
}
```

The above GraphQL query returns the JSON result below. It handles all
kinds of complexity without you having to writing a line of code. 

For example there is a while greater than `gt` and a limit clause on a child field. And the `avatar` field is renamed to `picture`. The `password` field is blocked and not returned. Finally the relationship between the `users` table and the `products` table is auto discovered and used.

#### JSON Result

```json
{
  "data": {
    "users": [
      {
        "id": 1,
        "email": "odilia@west.info",
        "picture": "https://robohash.org/simur.png?size=300x300",
        "full_name": "Edwin Orn",
        "products": [
          {
            "id": 16,
            "name": "Sierra Nevada Style Ale",
            "description": "Belgian Abbey, 92 IBU, 4.7%, 17.4°Blg",
            "price": 16.47
          },
          ...
        ]
      }
    ]
  }
}
```

#### Try with an authenticated user

In development mode you can use the `X-User-ID: 4` header to set a user id so you don't have to worries about cookies etc. This can be set using the *HTTP Headers* tab at the bottom of the web UI you'll see when you visit the above link. You can also directly run queries from the commandline like below.

#### Querying the GQL endpoint

```bash

# fetch the response json directly from the endpoint using user id 5
curl 'http://localhost:8080/api/v1/graphql' \
  -H 'content-type: application/json' \
  -H 'X-User-ID: 5' \
  --data-binary '{"query":"{ products { name price users { email }}}"}'

```

## How to GraphQL

GraphQL (GQL) is a simple query syntax that's fast replacing REST APIs. GQL is great since it allows web developers to fetch the exact data that they need without depending on changes to backend code. Also if you squint hard enough it looks a little bit like JSON :smiley:

The below query will fetch an `users` name, email and avatar image (renamed as picture). If you also need the users `id` then just add it to the query.

```graphql
query {
  user {
    full_name
    email
    picture : avatar
  }
}
```

### Fetching data

To fetch a specific `product` by it's ID you can use the `id` argument. The real name id field will be resolved automatically so this query will work even if your id column is named something like `product_id`.

```graphql
query {
  products(id: 3) {
    name
  }
}
```

Postgres also supports full text search using a TSV index. Super Graph makes it easy to use this full text search capability using the `search` argument.

```graphql
query {
  products(search "amazing") {
    name
  }
}
```

### Complex queries (Where)

Super Graph support complex queries where you can add filters, ordering,offsets and limits on the query.

#### Logical Operators

Name | Example | Explained |
--- | --- | --- |
and | price : { and : { gt: 10.5, lt: 20 } | price > 10.5 AND price < 20
or |  or : { price : { greater_than : 20 }, quantity: { gt : 0 } }  | price >= 20 OR quantity > 0
not | not: { or : { quantity : { eq: 0 }, price : { eq: 0 } } } | NOT (quantity = 0 OR price = 0)

#### Other conditions

Name | Example | Explained |
--- | --- | --- |
eq, equals | id : { eq: 100 } | id = 100 
neq, not_equals | id: { not_equals: 100 } | id != 100
gt, greater_than | id: { gt: 100 } | id > 100
lt, lesser_than | id: { gt: 100 } | id < 100
gte, greater_or_equals | id: { gte: 100 } | id >= 100
lte, lesser_or_equals | id: { lesser_or_equals: 100 } | id <= 100
in | status: { in: [ "A", "B", "C" ] } | status IN ('A', 'B', 'C)
nin, not_in | status: { in: [ "A", "B", "C" ] } | status IN ('A', 'B', 'C)
like | name: { like "phil%" } | Names starting with 'phil'
nlike, not_like | name: { nlike "v%m" } | Not names starting with 'v' and ending with 'm'
ilike | name: { ilike "%wOn" } | Names ending with 'won' case-insensitive
nilike, not_ilike | name: { nilike "%wOn" } | Not names ending with 'won' case-insensitive
similar | name: { similar: "%(b\|d)%" } | [Similar Docs](https://www.postgresql.org/docs/9/functions-matching.html#FUNCTIONS-SIMILARTO-REGEXP)
nsimilar, not_similar | name: { nsimilar: "%(b\|d)%" } | [Not Similar Docs](https://www.postgresql.org/docs/9/functions-matching.html#FUNCTIONS-SIMILARTO-REGEXP)
has_key | column: { has_key: 'b' } | Does JSON column contain this key
has_key_any | column: { has_key_any: [ a, b ] } | Does JSON column contain any of these keys
has_key_all | column: [ a, b ] | Does JSON column contain all of this keys
contains | column: { contains: [1, 2, 4] } | Is this array/json column a subset of value
contained_in | column: { contains: "{'a':1, 'b':2}" } | Is this array/json column a subset of these value
is_null | column: { is_null: true } | Is column value null or not

### Aggregation (Max, Count, etc)

You will often find the need to fetch aggregated values from the database such as `count`, `max`, `min`, etc. This is simple to do with GraphQL, just prefix the aggregation name to the field name that you want to aggregrate like `count_id`. The below query will group products by name and find the minimum price for each group. Notice the `min_price` field we're adding `min_` to price.

```graphql
query {
  products {
    name
    min_price
  }
}
```

Name | Explained |
--- | --- |
avg | Average value
count | Count the values
max | Maximum value
min | Minimum  value
stddev | [Standard Deviation](https://en.wikipedia.org/wiki/Standard_deviation)
stddev_pop | Population Standard Deviation
stddev_samp | Sample Standard Deviation
variance | [Variance](https://en.wikipedia.org/wiki/Variance)
var_pop | Population Standard Variance
var_samp | Sample Standard variance

All kinds of queries are possible with GraphQL. Below is an example that uses a lot of the features available. Comments `# hello` are also valid within queries.

```graphql
query {
  products(
    # returns only 30 items
    limit: 30,

    # starts from item 10, commented out for now
    # offset: 10,

    # orders the response items by highest price
    order_by: { price: desc },

    # no duplicate prices returned
    distinct: [ price ]
    
    # only items with an id >= 30 and < 30 are returned
    where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) {
    id
    name
    price
  }
}
```

### Full text search

Every app these days needs search. Enought his often means reaching for something heavy like Solr. While this will work why add complexity to your infrastructure when Postgres has really great
and fast full text search built-in. And since it's part of Postgres it's also available in Super Graph.

```graphql
query {
  products(
    # Search for all products that contain 'ale' or some version of it
    search: "ale"

    # Return only matches where the price is less than 10
    where: { price: { lt: 10 } }
    
    # Use the search_rank to order from the best match to the worst
    order_by: { search_rank: desc }) {
    id
    name
    search_rank
   	search_headline_description
  }
}
```

This query will use the `tsvector` column in your database table to search for products that contain the query phrase or some version of it. To get the internal relevance ranking for the search results using the `search_rank` field. And to get the highlighted context within any of the table columns you can use the `search_headline_` field prefix. For example `search_headline_name` will return the contents of the products name column which contains the matching query marked with the `<b></b>` html tags.

```json
{
  "data": {
    "products": [
      {
        "id": 11,
        "name": "Maharaj",
        "search_rank": 0.243171,
        "search_headline_description": "Blue Moon, Vegetable Beer, Willamette, 1007 - German <b>Ale</b>, 48 IBU, 7.9%, 11.8°Blg"
      },
      {
        "id": 12,
        "name": "Schneider Aventinus",
        "search_rank": 0.243171,
        "search_headline_description": "Dos Equis, Wood-aged Beer, Magnum, 1099 - Whitbread <b>Ale</b>, 15 IBU, 9.5%, 13.0°Blg"
      },
  ...
```

#### Adding search to your Rails app

It's really easy to enable Postgres search on any table within your database schema. All it takes is to create the following migration. In the below example we add a full-text search to the `products` table.

```ruby
class AddSearchColumn < ActiveRecord::Migration[5.1]
  def self.up
    add_column :products, :tsv, :tsvector
    add_index :products, :tsv, using: "gin"

    say_with_time("Adding trigger to update the ts_vector column") do
      execute <<-SQL
        CREATE FUNCTION products_tsv_trigger() RETURNS trigger AS $$
        begin
          new.tsv :=
          setweight(to_tsvector('pg_catalog.english', coalesce(new.name,'')), 'A') ||
          setweight(to_tsvector('pg_catalog.english', coalesce(new.description,'')), 'B');
          return new;
        end
        $$ LANGUAGE plpgsql;

        CREATE TRIGGER tsvectorupdate BEFORE INSERT OR UPDATE ON products FOR EACH ROW EXECUTE PROCEDURE products_tsv_trigger();
        SQL
      end
  end

  def self.down
    say_with_time("Removing trigger to update the tsv column") do
      execute <<-SQL
        DROP TRIGGER tsvectorupdate
        ON products
        SQL
    end

    remove_index :products, :tsv
    remove_column :products, :tsv
  end
end
```

## Authentication

You can only have one type of auth enabled. You can either pick Rails or JWT. Uncomment the one you use and leave the rest commented out.

### Rails Auth (Devise / Warden)

Almost all Rails apps use Devise or Warden for authentication. Once the user is 
authenticated a session is created with the users ID. The session can either be
stored in the users browser as a cookie, memcache or redis. If memcache or redis is used then a cookie is set in the users browser with just the session id. 

Super Graph can handle all these variations including the old and new session formats. Just enable the right `auth` config based on how your rails app is configured.

#### Cookie session store

```yaml
auth:
  type: rails_cookie
  cookie: _app_session

  rails_cookie:
    secret_key_base: caf335bfcfdb04e50db5bb0a4d67ab9...
```

#### Memcache session store

```yaml
auth:
  type: rails_memcache
  cookie: _app_session

  rails_memcache:
    host: 127.0.0.1
```

#### Redis session store

```yaml
auth:
  type: rails_redis
  cookie: _app_session

  rails_redis:
    url: redis://127.0.0.1:6379
    password: ""
    max_idle: 80
    max_active: 12000
```

### JWT Token Auth

```yaml
auth:
  type: jwt
  cookie: _app_session
  
  jwt:
    provider: auth0 #none
    secret: abc335bfcfdb04e50db5bb0a4d67ab9
    public_key_file: /secrets/public_key.pem
    public_key_type: ecdsa #rsa
```

For JWT tokens we currently support tokens from a provider like Auth0
or if you have a custom solution then we look for the `user_id` in the
`subject` claim of of the `id token`. If you pick Auth0 then we derive two variables from the token `user_id` and `user_id_provider` for to use in your filters.

We can get the JWT token either from the `authorization` header where we expect it to be a `bearer` token or if `cookie` is specified then we look there.

For validation a `secret` or a public key (ecdsa or rsa) is required. When using public keys they have to be in a PEM format file.

## Easy to setup

Configuration files can either be in YAML or JSON their names are derived from the `GO_ENV` variable, for example `GO_ENV=prod` will cause the `prod.yaml` config file to be used. or `GO_ENV=dev` will use the `dev.yaml`. A path to look for the config files in can be specified using the `-path <folder>` command line argument.

We're tried to ensure that the config file is self documenting and easy to work with.

```yaml
title: Super Graph Development
host_port: 0.0.0.0:8080
web_ui: true
debug_level: 1
enable_tracing: false

# Throw a 401 on auth failure for queries that need auth
# valid values: always, per_query, never
auth_fail_block: never

# Postgres related environment Variables
# SG_DATABASE_HOST
# SG_DATABASE_PORT
# SG_DATABASE_USER
# SG_DATABASE_PASSWORD

# Auth related environment Variables
# SG_AUTH_RAILS_COOKIE_SECRET_KEY_BASE
# SG_AUTH_RAILS_REDIS_URL
# SG_AUTH_RAILS_REDIS_PASSWORD
# SG_AUTH_JWT_PUBLIC_KEY_FILE

# inflections:
#   person: people
#   sheep: sheep

auth: 
  # Can be 'rails' or 'jwt'
  type: rails
  cookie: _app_session

  # Comment this out if you want to disable setting
  # the user_id via a header. Good for testing
  header: X-User-ID

  rails:
    # Rails version this is used for reading the
    # various cookies formats.
    version: 5.2

    # Found in 'Rails.application.config.secret_key_base'
    secret_key_base: 0a248500a64c01184edb4d7ad3a805488f8097ac761b76aaa6c17c01dcb7af03a2f18ba61b2868134b9c7b79a122bc0dadff4367414a2d173297bfea92be5566
    
    # Remote cookie store. (memcache or redis)
    # url: redis://127.0.0.1:6379
    # password: test
    # max_idle: 80,
    # max_active: 12000,

    # In most cases you don't need these
    # salt: "encrypted cookie"
    # sign_salt: "signed encrypted cookie"
    # auth_salt: "authenticated encrypted cookie"

  # jwt:
  #   provider: auth0
  #   secret: abc335bfcfdb04e50db5bb0a4d67ab9
  #   public_key_file: /secrets/public_key.pem
  #   public_key_type: ecdsa #rsa

database:
  type: postgres
  host: db
  port: 5432
  dbname: app_development
  user: postgres
  password: ''
  # pool_size: 10
  # max_retries: 0
  # log_level: "debug" 

  # Define variables here that you want to use in filters 
  variables:
    account_id: "select account_id from users where id = $user_id"

  # Define defaults to for the field key and values below
  defaults:
    filter: ["{ user_id: { eq: $user_id } }"]
    
    # Fields and table names that you wish to block
    blacklist:
      - ar_internal_metadata
      - schema_migrations
      - secret
      - password
      - encrypted
      - token

  fields:
    - name: users
      # This filter will overwrite defaults.filter
      filter: ["{ id: { eq: $user_id } }"]

    - name: products
      # Multiple filters are AND'd together
      filter: [
        "{ price: { gt: 0 } }",
        "{ price: { lt: 8 } }"
      ] 

    - name: customers
      # No filter is used for this field not 
      # even defaults.filter
      filter: none

    - # You can create new fields that have a
      # real db table backing them
      name: me
      table: users
      filter: ["{ id: { eq: $user_id } }"]

    # - name: posts
    #   filter: ["{ account_id: { _eq: $account_id } }"]
```

If deploying into environments like Kubernetes it's useful to be able to configure things like secrets and hosts though environment variables therfore we expose the below environment variables. This is escpecially useful for secrets since they are usually injected in via a secrets management framework ie. Kubernetes Secrets

Keep in mind any value can be overwritten using environment variables for example `auth.jwt.public_key_type` converts to `SG_AUTH_JWT_PUBLIC_KEY_TYPE`. In short prefix `SG_`, upper case and all `.` should changed to `_`.

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

## Deploying Super Graph

How do I deploy the Super Graph service with my existing rails app? You have several options here. Esentially you need to ensure your app's session cookie will be passed to this service. 

### Custom Docker Image

Create a `Dockerfile` like the one below to roll your own
custom Super Graph docker image. And to build it `docker build -t my-super-graph .`

```docker
FROM dosco/super-graph:latest
WORKDIR /app
COPY *.yml ./
```

### Deploy under a subdomain
For this to work you have to ensure that the option `:domain => :all` is added to your rails app config `Application.config.session_store` this will cause your rails app to create session cookies that can be shared with sub-domains. More info here <http://excid3.com/blog/sharing-a-devise-user-session-across-subdomains-with-rails-3/>

### With an NGINX loadbalancer
I'm sure you know how to configure it so that the Super Graph endpoint path `/api/v1/graphql` is routed to wherever you have this service installed within your architecture.

### On Kubernetes
If your Rails app runs on Kubernetes then ensure you have an ingress config deployed that points the path to the service that you have deployed Super Graph under.

### JWT tokens (Auth0, etc)
In that case deploy under a subdomain and configure this service to use JWT authentication. You will need the public key file or secret key. Ensure your web app passes the JWT token with every GQL request in the Authorize header as a `bearer` token.

## Developing Super Graph 

If you want to build and run Super Graph from code then the below commands will build the web ui and launch Super Graph in developer mode with a watcher to rebuild on code changes. And the demo rails app is also launched to make it essier to test changes.

```bash

# yarn is needed to build the web ui
brew install yarn

# yarn install dependencies and build the web ui
(cd web && yarn install && yarn build)

# generate some stuff the go code needs
go generate ./...

# do this the only the time to setup the database
docker-compose run web rake db:create db:migrate

# start super graph in development mode with a change watcher
docker-compose up

```


## MIT License

MIT Licensed | Copyright © 2018-present Vikram Rangnekar