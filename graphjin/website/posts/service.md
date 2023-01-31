---
chapter: 8
title: Service
description: Using GraphJin as a standlone service
---

# Service

<mark>GraphJin Standalone is a docker packaged GO build that you can run as a service on it's own.</mark> It exposes **REST, GraphQL and Websocket** APIs and can handle various authentications like JWT, Rails cookies, etc.

It is very fast, secure and has a ton of API best practices built in like `Rate Limiting`, `ETags & Cache Headers`, `Compression`, etc

#### TOC

### Trying out the example

For this example we will create a [example e-commerce store](https://github.com/dosco/graphjin/tree/master/examples/webshop). This example app can be found in repo.

Below we explain how this example app was built and other details around useing GraphJin to make you more productive.

```shell
git clone https://github.com/dosco/graphjin.git
cd graphjin/examples/webshop
docker compose run api db setup
docker compose up
```

### Install GraphJin

#### 1. Quick Install

```shell
# Mac (Homebrew)
brew install dosco/graphjin/graphjin

# Ubuntu (Snap)
sudo snap install --classic graphjin
```

Debian and Redhat ([releases](https://github.com/dosco/graphjin/releases)) download the .deb or .rpm from the releases page and install with dpkg -i and rpm -i respectively.

#### 2. Create a new API

Let's call our app Webshop.

```shell
graphjin new webshop
cd webshop
```

### Add a database schema

```shell
graphjin db migrate new users
graphjin db migrate new products
graphjin db migrate new sections
graphjin db migrate new customers
graphjin db migrate new purchases
graphjin db migrate new notifications

# delete the example migration
rm -rf config/migrations/0_init.sql
```

### Defines tables and database relationships

Be sure to define primary keys to all of your tables and to use foreign keys to define
relationships between tables. In the example below the products table has a foreign key relationhsip `user_id` to the users table. It looks like this `user_id bigint REFERENCES users(id)`

```sql title="Database Migration config/migrations/1_products.sql"
-- Write your migrate up statements here

CREATE TABLE products (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    descriptioj TEXT,
    user_id BIGINT REFERENCES users(id)
);
```

### Create a seed script

This step is optional. A seed script inserts fake data into the database. It helps frontend developers if they already have some fake data to work with. The seed script is written in javascript and data is inserted into the database using GraphQL.

```js title="Seed Script"
// Ceate 100 fake users
for (i = 0; i < 100; i++) {
  // Fake data functions are built into GraphJin
  var data = {
    full_name: fake.name(),
    avatar: fake.avatar_url(),
    phone: fake.phone(),
    email: "user" + i + "@demo.com",
  };

  // Graphql mutation query to insert user
  var res = graphql(
    "mutation { users(insert: $data) { id } }",
    { data: data },
    { user_id: -1 }
  );
}
```

### Setup the database

Ensure you have a Postgres database running and the config file has the correct connection details to it.

```shell
graphjin db setup
```

### Start the Webshop API

```shell
graphjin serve
```

### GraphJin WebUI

The GraphJin web UI is used to build and test queries. It supports auto-completion which makes it easy to craft queries. Open your web browser and visit the below url.

[http://localhost:8080](http://localhost:8080)

### Fetch data with GraphQL

```graphql
query getProducts {
  products {
    id
    name
    description
    customers {
      id
      email
    }
  }
}
```

```json title="Result"
{
  "data": {
    "products": [
      {
        "id": 1,
        "name": "Oak Aged Yeti Imperial Stout",
        "customers": [
          {
            "id": 2,
            "email": "johannahagenes@considine.com"
          },
          {
            "id": 2,
            "email": "johannahagenes@considine.com"
          }
        ],
        "description": "Belgian And French Ale, Galena, Carapils"
      },
      ...
    ]
  }
}
```

### Secrets management

We recommend you use [Mozilla SOPS](https://github.com/mozilla/sops) for secrets management. The sops binary is installed on the GraphJin app docker image. To use SOPS you create a yaml file with your secrets like the one below. You then need a secret key to encrypt it. Your options are to go with Google Cloud KMS, Amazon KMS, Azure Key Vault, etc. In production SOPS will automatically fetch the key from your defined KMS, decrypt the secrets file and make the values available to GraphJin via enviroment variables.

1. Create the secrets file

```yml
SG_DATABASE_PASSWORD: postgres
SG_AUTH_JWT_SECRET: jwt_token_secret_key
SG_SECRET_KEY: generic_secret_ke
```

2. Login to your cloud (Google Example)

```shell
gcloud auth login
gcloud auth application-default login
```

3. Encrypt the secrets with the key

```shell
sops -e -i ./config/prod.secrets.yml
```

### Authentication

You can only have one type of auth enabled either Rails or JWT.

#### Ruby on Rails

Almost all Rails apps use Devise or Warden for authentication. Once the user is
authenticated a session is created with the users ID. The session can either be
stored in the users browser as a cookie, memcache or redis. If memcache or redis is used then a cookie is set in the users browser with just the session id.

GraphJin can handle all these variations including the old and new session formats. Just enable the right `auth` config based on how your rails app is configured.

#### Cookie session store

```yaml
auth:
  type: rails
  cookie: _app_session

  rails:
    # Rails version this is used for reading the
    # various cookies formats.
    version: 5.2

    # Found in 'Rails.application.config.secret_key_base'
    secret_key_base: 0a248500a64c01184edb4d7ad3a805488f8097ac761b76aaa6c17c01dcb7af03a2f18ba61b2868134b9c7b79a122bc0dadff4367414a2d173297bfea92be5566
```

#### Memcache session store

```yaml
auth:
  type: rails
  cookie: _app_session

  rails:
    # Memcache remote cookie store.
    url: memcache://127.0.0.1
```

#### Redis session store

```yaml
auth:
  type: rails
  cookie: _app_session

  rails:
    # Redis remote cookie store
    url: redis://127.0.0.1:6379
    password: ""
    max_idle: 80
    max_active: 12000
```

#### JWT Tokens

```yaml
auth:
  type: jwt

 jwt:
    # valid providers are auth0, firebase, jwks and none
    provider: auth0
    secret: abc335bfcfdb04e50db5bb0a4d67ab9
    public_key_file: /secrets/public_key.pem
    public_key_type: ecdsa #rsa
    issuer: https://my-domain.auth0.com
    audience: my_client_id
```

For JWT tokens we currently support tokens from a provider like Auth0 or if you have a custom solution then we look for the `user_id` in the `subject` claim of of the `id token`. If you pick Auth0 then we derive two variables from the token `user_id` and `user_id_provider` for to use in your filters.

We can get the JWT token either from the `authorization` header where we expect it to be a `bearer` token or if `cookie` is specified then we look there.

For validation a `secret` or a public key (ecdsa or rsa) is required. When using public keys they have to be in a PEM format file.

Setting `issuer` is recommended but not required. When specified it's going to be compared against the `iss` claim of the JWT token.

Also `audience` is recommended but not required. When specified it's going to be compared against the `aud` claim of the JWT token. The `aud` claim usually identifies the intended recipient of the token. For Auth0 is the client_id, for other provider could be the domain URL.

#### Firebase Auth

```yaml
auth:
  type: jwt

  jwt:
    provider: firebase
    audience: <firebase-project-id>
```

Firebase auth also uses JWT the keys are auto-fetched from Google and used according to their documentation mechanism. The `audience` config value needs to be set to your project id and everything else is taken care for you.

Setting `issuer` is not required for Firebase, it's going to be automatically defined using the `audience` as "https://securetoken.google.com/<audience>".

#### JWKS Auth

```yaml
auth:
  type: jwt

  jwt:
    provider: jwks
    issuer: https://accounts.google.com
    audience: 1234987819200.apps.googleusercontent.com
    jwks_url: https://www.googleapis.com/oauth2/v3/certs
    jwks_min_refresh: 30
```

The JWKS provider downloads and keeps track of keys which are automatically refreshed from a JWKS endpoint, like "https://YOUR_DOMAIN/.well-known/jwks.json".

Interval between refreshes could be calculated in two ways:

1. You can set an explicit refresh interval in minutes by using `jwks_refresh`. In this mode, it doesn't matter what the HTTP response says in its Cache-Control or Expires headers.
2. If `jwks_refresh` is not defined, then the time to refresh is automatically calculated based on the key's Cache-Control or Expires headers. You could define an absolute minimum interval before refreshes in minutes with `jwks_min_refresh`. This value is used as a fallback value when tokens are refreshed, if unspecified, the minimum refresh interval is 60 minutes.

We can get the JWT token either from the `authorization` header where we expect it to be a `bearer` token or if `cookie` is specified then we look there.

Setting `issuer` is recommended but not required. When specified it's going to be compared against the `iss` claim of the JWT token.

Also `audience` is recommended but not required. When specified it's going to be compared against the `aud` claim of the JWT token. The `aud` claim usually identifies the intended recipient of the token. For Auth0 is the client_id, for other provider could be the domain URL.

#### HTTP Headers

```yaml
header:
  name: X-AppEngine-QueueName
  exists: true
  #value: default
```

Header auth is usually the best option to authenticate requests to the action endpoints. For example you
might want to use an action to refresh a materalized view every hour and only want a cron service like the Google AppEngine Cron service to make that request in this case a config similar to the one above will do.

The `exists: true` parameter ensures that only the existance of the header is checked not its value. The `value` parameter lets you confirm that the value matches the one assgined to the parameter. This helps in the case you are using a shared secret to protect the endpoint.
