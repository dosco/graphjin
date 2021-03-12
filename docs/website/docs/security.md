---
id: security
title: API Security
sidebar_label: API Security
---

One of the the most common questions I get asked is what happens if a user out on the internet sends queries
that we don't want run. For example how do we stop him from fetching all users or the emails of users. Our answer to this is that it is not an issue as this cannot happen, let me explain.

GraphJin runs in one of two modes `development` or `production`, this is controlled via the config value `production: false` when it's false it's running in development mode and when true, production. In development mode all the **named** queries (including mutations) are saved to the allow list `./config/allow.list`. While in production mode when GraphJin starts only the queries from this allow list file are registered with the database as [prepared statements](https://stackoverflow.com/questions/8263371/how-can-prepared-statements-protect-from-sql-injection-attacks).

Prepared statements are designed by databases to be fast and secure. They protect against all kinds of sql injection attacks and since they are pre-processed and pre-planned they are much faster to run then raw sql queries. Also there's no GraphQL to SQL compiling happening in production mode which makes your queries lighting fast as they are directly sent to the database with almost no overhead.

In short in production only queries listed in the allow list file `./config/allow.list` can be used, all other queries will be blocked.

:::tip How to think about the allow list?
The allow list file is essentially a list of all your exposed API calls and the data that passes within them. It's very easy to build tooling to do things like parsing this file within your tests to ensure fields like `credit_card_no` are not accidently leaked. It's a great way to build compliance tooling and ensure your user data is always safe.
:::

This is an example of a named query, `getUserWithProducts` is the name you've given to this query it can be anything you like but should be unique across all you're queries. Only named queries are saved in the allow list in development mode.

```graphql
query getUserWithProducts {
  users {
    id
    name
    products {
      id
      name
      price
    }
  }
}
```

## Authentication

You can only have one type of auth enabled either Rails or JWT.

### Ruby on Rails

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

### JWT Tokens

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

### Firebase Auth

```yaml
auth:
  type: jwt

  jwt:
    provider: firebase
    audience: <firebase-project-id>
```

Firebase auth also uses JWT the keys are auto-fetched from Google and used according to their documentation mechanism. The `audience` config value needs to be set to your project id and everything else is taken care for you.

Setting `issuer` is not required for Firebase, it's going to be automatically defined using the `audience` as "https://securetoken.google.com/<audience>".

### JWKS Auth

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

1) You can set an explicit refresh interval in minutes by using `jwks_refresh`. In this mode, it doesn't matter what the HTTP response says in its Cache-Control or Expires headers.
2) If `jwks_refresh` is not defined, then the time to refresh is automatically calculated based on the key's Cache-Control or Expires headers. You could define an absolute minimum interval before refreshes in minutes with `jwks_min_refresh`. This value is used as a fallback value when tokens are refreshed, if unspecified, the minimum refresh interval is 60 minutes.

We can get the JWT token either from the `authorization` header where we expect it to be a `bearer` token or if `cookie` is specified then we look there.

Setting `issuer` is recommended but not required. When specified it's going to be compared against the `iss` claim of the JWT token.

Also `audience` is recommended but not required. When specified it's going to be compared against the `aud` claim of the JWT token. The `aud` claim usually identifies the intended recipient of the token. For Auth0 is the client_id, for other provider could be the domain URL.

### HTTP Headers

```yaml
header:
  name: X-AppEngine-QueueName
  exists: true
  #value: default
```

Header auth is usually the best option to authenticate requests to the action endpoints. For example you
might want to use an action to refresh a materalized view every hour and only want a cron service like the Google AppEngine Cron service to make that request in this case a config similar to the one above will do.

The `exists: true` parameter ensures that only the existance of the header is checked not its value. The `value` parameter lets you confirm that the value matches the one assgined to the parameter. This helps in the case you are using a shared secret to protect the endpoint.

### Named Auth

```yaml
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
```

In addition to the default auth configuration you can create additional named auth configurations to be used
with features like `actions`. For example while your main GraphQL endpoint uses JWT for authentication you may want to use a header value to ensure your actions can only be called by clients having access to a shared secret
or security header.

### Testing Subscription Authentication

Sending HTTP headers is not allowed over WebSocket, so it has to be done during the `connection_init` message over WebSocket sending the headers inside the JSON payload of the message.

If you want to test the Authentication of the subscriptions with an standard client that is not able to pass the headers over WebSocket, you could set the `subs_creds_in_vars` param inside the `auth` block of the config file and define the headers inside the variables of the subscription.

```yml
auth:
  # Useful for quickly debugging subscriptions WebSocket authorization.
  # Disable in production
  subs_creds_in_vars: true
```

Variables:

```json
{
  "example-var": 20,
  "X-User-ID": "1",
  "X-User-ID-Provider": "testing-provider",
  "X-User-Role": "testing-role"
}
```

## Actions

Actions is a very useful feature that is currently work in progress. For now the best use case for actions is to
refresh database tables like materialized views or call a database procedure to refresh a cache table, etc. An action creates an http endpoint that anyone can call to have the SQL query executed. The below example will create an endpoint `/api/v1/actions/refresh_leaderboard_users` any request send to that endpoint will cause the sql query to be executed. the `auth_name` points to a named auth that should be used to secure this endpoint. In future we have big plans to allow your own custom code to run using actions.

```yaml
actions:
  - name: refresh_leaderboard_users
    sql: REFRESH MATERIALIZED VIEW CONCURRENTLY "leaderboard_users"
    auth_name: from_taskqueue
```

#### Using CURL to test a query

```bash
# fetch the response json directly from the endpoint using user id 5
curl 'http://localhost:8080/api/v1/graphql' \
  -H 'content-type: application/json' \
  -H 'X-User-ID: 5' \
  --data-binary '{"query":"{ products { name price users { email }}}"}'
```

## Access Control

It's common for APIs to control what information they return or insert based on the role of the user. In GraphJin we have two primary roles `user` and `anon` the first for users where a `user_id` is available the latter for users where it's not.

:::info Define authenticated request?
An authenticated request is one where GraphJin can extract an `user_id` based on the configured authentication method (jwt, rails cookies, etc).
:::

The `user` role can be divided up into further roles based on attributes in the database. For example when fetching a list of users, a normal user can only fetch his own entry while an admin can fetch all the users within a company and an admin user can fetch everyone. In some places this is called Attribute based access control. So in way we support both. Role based access control and Attribute based access control.

GraphJin allows you to create roles dynamically using a `roles_query` and `match` config values.

### Configure RBAC

```yaml
roles_query: "SELECT * FROM users WHERE users.id = $user_id"

roles:
  - name: user
    tables:
      - name: users
        query:
          filters: ["{ id: { _eq: $user_id } }"]

        insert:
          filters: ["{ user_id: { eq: $user_id } }"]
          columns: ["id", "name", "description"]
          presets:
            - created_at: "now"

        update:
          filters: ["{ user_id: { eq: $user_id } }"]
          columns:
            - id
            - name
          presets:
            - updated_at: "now"

        delete:
          block: true

  - name: admin
    match: users.id = 1
    tables:
      - name: users
        query:
          filters: []
```

This configuration is relatively simple to follow the `roles_query` parameter is the query that must be run to help figure out a users role. This query can be as complex as you like and include joins with other tables.

The individual roles are defined under the `roles` parameter and this includes each table the role has a custom setting for. The role is dynamically matched using the `match` parameter for example in the above case `users.id = 1` means that when the `roles_query` is executed a user with the id `1` will be assigned the admin role and those that don't match get the `user` role if authenticated successfully or the `anon` role.
