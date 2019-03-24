# Super Graph

## Instant GraphQL API for Rails. Zero code.

Get an instant high-performance GraphQL API for your Rails apps in seconds. Super Graph will auto learn your database structure and relationships. Built in support for Rails authentication and for JWT tokens.

## Back story and motivation

I have a Rails app that gets a bit of traffic. Having planned to improve the UI using React or Vue I found that my current APIs didn't have the data I needed. I was too lazy to build new controllers. My controllers were esentially wrappers around database queries and I didn't enjoy having to figure out new REST APIs with paths, names and methods to fetch all this new data.

I always liked GraphQL and how simplifies things for web devs. On the backend however GraphQL seemed overly complex as it still required me to write a lot of the same database query code. I wanted a GraphQL server that just worked the second you deployed it without having to write a line of code.

And so after a lot of coffee and some avocado toasts we now have Super Graph, an instant GraphQL API that is high performance and quick to deploy. One service to rule all your database querying needs.

## Features
- Support for Rails database conventions
- Belongs-To, One-To-Many and Many-To-Many table relationships
- Devise, Warden encrypted and signed session cookies
- Redis, Memcache and Cookie session stores
- Generates highly optimized Postgres SQL quries
- Customize through a simple config file
- High performance GoLang codebase
- Tiny docker image and low memory requirements

### GraphQL (GQL)

We currently support the `query` action which is used for fetching data. Support
for `mutation` and `subscriptions` is currently work in progress. For example the below query fetches two products that belong to the current user where the price is greater than 10

#### GQL Query

```gql
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

The above GQL query returns the JSON result below. It handles all
kinds of complexity without you writing a line of code. For example there is a while greater than `gt` and a limit clause on a child field. And the `avatar` field is renamed to `picture`. The `password` field is blocked and not returned. Finally the relationship between the `users` table and the `products` table is auto discovered and used.


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

## Try it out

Please be patient on the first run Go has to download packages and this
can be a little slow.

```console
$ docker-compose run web rake db:create db:migrate db:seed
$ docker-compose up
$ open http://localhost:8080
```

In development mode you can use the `X-User-ID: 4` header to set a user id so you don't have to worries about cookies etc. This can be set using the *HTTP Headers* tab at the bottom of the web UI you'll see when you visit the above link. You can also directly run quries from the commandline like shown below.

#### Querying the GQL endpoint

```console
curl 'http://localhost:8080/api/v1/graphql' \
  -H 'content-type: application/json' \
  -H 'X-User-ID: 5' \
  --data-binary '{"query":"{ products { name price users { email }}}"}'
```

## How to GQL

GQL is a simple query language that is fast replacing REST APIs. GQL is great
since it allows web developers to fetch the exact data that they need without 
depending on changes to backend code.

The below query will fetch a `users` name, email and avatar image renamed as picture. If you also need the users `id` then just add it to the query.

```gql
query {
  user {
    full_name
    email
    picture : avatar
  }
}
```

Super Graph support complex quries where you can add filters, ordering, offsets and limits on the query.

```javascript
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

## Web UI for web developers
![Super Graph Web UI](web/public/super-graph-web-ui.png?raw=true "Super Graph Web UI for web developers")

## Configuration

Config files can either be in YAML or JSON their names are derived from the `GO_ENV` variable, for example `GO_ENV=prod` will cause the `prod.yaml` config file to be used. or `GO_ENV=dev` will use the `dev.yaml`. A path to the config files can be specified using the `-path <folder>` command line argument.

```yaml
host_port: 0.0.0.0:8080
web_ui: true
debug_level: 1

# When to throw a 401 on auth failure 
# valid values: always, per_query, never
auth_fail_block: never

# Postgres related enviroment Variables
# SG_DATABASE_HOST
# SG_DATABASE_PORT
# SG_DATABASE_USER
# SG_DATABASE_PASSWORD

# Auth related enviroment Variables
# SG_AUTH_SECRET_KEY_BASE
# SG_AUTH_PUBLIC_KEY_FILE
# SG_AUTH_URL
# SG_AUTH_PASSWORD

# inflections:
#   person: people
#   sheep: sheep

auth:
  type: header
  field_name: X-User-ID

# auth:
#   type: rails
#   cookie: _app_session
#   store: cookie
#   secret_key_base: caf335bfcfdb04e50db5bb0a4d67ab9...

# auth:
#   type: rails
#   cookie: _app_session
#   store: memcache
#   host: 127.0.0.1

# auth:
#   type: rails
#   cookie: _app_session
#   store: redis
#   max_idle: 80,
#   max_active: 12000,
#   url: redis://127.0.0.1:6379
#   password: ""

# auth:
#   type: jwt
#   cookie: _app_session
#   secret: abc335bfcfdb04e50db5bb0a4d67ab9
#   public_key_file: abc335bfcfdb04e50db5bb0a4d67ab9
#   public_key_type: ecdsa #rsa

database:
  type: postgres
  host: db
  port: 5432
  dbname: app_development
  user: postgres
  password: ''
  #pool_size: 10
  #max_retries: 0
  #log_level: "debug" 

  # Define variables here that you want to use in filters 
  variables:
    account_id: "select account_id from users where id = $user_id"

  # Used to add access to tables 
  filters:
    users: "{ id: { _eq: $user_id } }"
    posts: "{ account_id: { _eq: $account_id } }"

  # Fields and table names that you wish to block
  blacklist:
    - secret
    - password
    - encrypted
    - token
```

If deploying into enviroments like Kubernetes it's useful to be able to configure things like secrets and hosts though enviroment variables so we expose the following. This is escpecially useful for secrets since they are usually injected in via a secrets management framework ie. Kubernetes Secrets

#### Postgres related enviroment Variables
```console
SG_DATABASE_HOST
SG_DATABASE_PORT
SG_DATABASE_USER
SG_DATABASE_PASSWORD
```

#### Auth related enviroment Variables
```console
SG_AUTH_SECRET_KEY_BASE
SG_AUTH_PUBLIC_KEY_FILE
SG_AUTH_URL
SG_AUTH_PASSWORD
```

## Deployment

How do I deploy the Super Graph service with my existing rails app? You have several options here. Esentially you need to ensure your app's session cookie
will be passed to this service. 

#### Deploy under a subdomain
For this to work you have to ensure that the option `:domain => :all` is added to your rails app config `Application.config.session_store` this will cause your rails app to create session cookies that can be shared with sub-domains. More info here <http://excid3.com/blog/sharing-a-devise-user-session-across-subdomains-with-rails-3/>

#### We have this NGINX loadbalancer
I'm sure you know how to configure it so that the graphql endpoint path is routed to wherever you have this service installed within your architecture.

#### On Kubernetes
If your existing rails app runs on Kubernetes then ensure you have an ingress config deployed that points the path `/api/v1/graphql` to the service that you have deployed Super Graph under.

#### We use JWT tokens like those from Auth0
In that case deploy under a subdomain and configure this service to use JWT authentication. You will need the public key file or secret key. Ensure your web app passes the JWT token with every GQL request in the Authorize header as a `bearer` token.

## Contact me

[twitter.com/dosco](https://twitter.com/dosco)

## License

[MIT](http://opensource.org/licenses/MIT)

Copyright (c) 2019-present Vikram Rangnekar


