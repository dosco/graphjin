---
id: example
title: Building an App from scratch
sidebar_label: Building an App
---

There are two parts to most web apps the backend and the frontends. The backend is usually an API of some kind and the frontends are mobile and web apps built in React, Vue, Android, iOS, etc.

Super Graph will help you instantly give you a powerful and high-performance GraphQL API that can be your apps backend. Let's get started, I promise you it's super easy and will save you weeks to months of your life.

For this example we will create a web e-commerce store. This example app can be found in repo. https://github.com/dosco/super-graph/tree/master/examples/webshop

## Try the example app

Below we explain how this example app was built and other details around useing Super Graph to make you more productive.

```console
git clone https://github.com/dosco/super-graph.git

cd super-graph/examples/webshop
docker-compose run api db:setup
docker-compose up
```

## Install Super Graph

```console
go get github.com/dosco/super-graph
```

## Create the app

Lets call our app Webshop

```console
super-graph new webshop
cd webshop
```

## Add a database schema

```console
super-graph db:new users
super-graph db:new products
super-graph db:new sections
super-graph db:new customers
super-graph db:new purchases
super-graph db:new notifications

# delete the example migration
rm -rf config/migrations/0_init.sql
```

## Defines tables and database relationships

Be sure to define primary keys to all the tables and use foreign keys to define
relationships between tables. In the example below the products table has a foreign key relationhsip `user_id` to the users table. It looks like this `user_id bigint REFERENCES users(id)`

```console
vim config/migrations/1_users.sql
```

```sql
-- Write your migrate up statements here

CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    full_name character varying NOT NULL,
    phone character varying,
    avatar character varying,
    email character varying NOT NULL DEFAULT ''::character varying,
    encrypted_password character varying NOT NULL DEFAULT ''::character varying,
    reset_password_token character varying,
    reset_password_sent_at timestamp without time zone,
    remember_created_at timestamp without time zone,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);

-- Indices -------------------------------------------------------

CREATE UNIQUE INDEX index_users_on_email ON users(email text_ops);
CREATE UNIQUE INDEX index_users_on_reset_password_token ON users(reset_password_token text_ops);


---- create above / drop below ----

DROP TABLE users;

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
```

```console
vim config/migrations/2_products.sql
```

```sql
-- Write your migrate up statements here

CREATE TABLE products (
    id BIGSERIAL PRIMARY KEY,
    name          text,
    description   text,
    price         numeric(7,2),
    tags          text[],
    category_ids  bigint[] NOT NULL,
    user_id       bigint REFERENCES users(id),
    created_at    timestamp without time zone NOT NULL,
    updated_at    timestamp without time zone NOT NULL,

    -- tsvector column needed for full-text search
    tsv tsvector GENERATED ALWAYS
      AS (to_tsvector('english', description)) STORED,
);

-- Indices -------------------------------------------------------

CREATE INDEX index_products_on_tsv ON products USING GIN (tsv tsvector_ops);
CREATE INDEX index_products_on_user_id ON products(user_id int8_ops);

---- create above / drop below ----

DROP TABLE products;

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above
```

## Create a seed script

This step is optional. A seed script inserts fake data into the database. It helps frontend developers if they already have some fake data to work with. The seed script is written in
javascript and data is inserted into the database using GraphQL.

If you don't plan to use it then delete the sample seed file `rm -rf config/seed.js`

```javascript
// Example script to seed database
var pwd = "12345";

// lets create 100 fake users
for (i = 0; i < 100; i++) {
  // these fake data functions are built into super graph
  var data = {
    full_name: fake.name(),
    avatar: fake.avatar_url(),
    phone: fake.phone(),
    email: "user" + i + "@demo.com",
    password: pwd,
    password_confirmation: pwd,
    created_at: "now",
    updated_at: "now",
  };

  // graphql mutation query to insert user
  var res = graphql(
    " \
	mutation { \
		user(insert: $data) { \
			id \
		} \
  }",
    // graphql variables like $data
    { data: data },
    // current authenicated user id
    { user_id: -1 }
  );
}
```

## Setup the database

This will create the database, run the migrations and the seed file. Once it's setup to reset use the `db:reset` commandline argument.

```console
docker-compose run api db:setup
```

## Start the Webshop API

```console
docker-compose up
```

## Access the Super Graph UI

The Super Graph web UI is used to build and test queries. It supports auto-completion which
makes it easy to craft queries. Open your web broser and visit the below url.

[http://localhost:8080](http://localhost:8080)

## Fetch data with GraphQL

```graphql
query {
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

```json
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

## Add validations

Validations can be added in the database schema this way nothing can bypass them
and put bad invalid data into your database.

```sql
CREATE TABLE products (
    name text CHECK (length(name) > 1 AND length(name) < 50),
    ...
)
```

## Full text search

Postgres has great full-text search built-in. No reason to complicate your setup by adding
another service just for search. Enabling full-text search on a table requires you to add
a single column (term-vector column) to it.

```sql
CREATE TABLE products (
    -- tsv column is used by full-text search
    tsv tsvector GENERATED ALWAYS
      AS (to_tsvector('english', name) || to_tsvector('english',description)) STORED,
    ...
```

## Autocomplete

Implementing autocomplete does not need a `tsv` column instead it needs an extra index
of the type `text_pattern_ops` on the volumn that you wish to autocomplete on.

```sql
CREATE INDEX product_name_autocomplete_index ON products(name text_pattern_ops);
```

Now use the below query and set the `$prefix` variable to the entered text value you want
to autocomplete on.

```graphql
query {
  products(
    where: { name: { ilike: $prefix } }
    order_by: { updated_at: desc }
  ) {
    ...Product
  }
}
```

## Using GraphQL fragments

Some of you unfamiliar with GraphQL might wonder what the `...Product` thing above is. It's called a fragment. Fragments save you from having to retype the same set of columns all over.
Most GraphQL clients like `apollo-client` or `urql` have a way to manage fragments and share then
across your queries.

```graphql
fragment Product on products {
  id
  name
  description
}

query {
  products(limit: 10) {
    ...Product
  }
}
```

## Create new product

The below GraphQL query and variables together will insert a new product into the database. The `connect` keyword will find a user who's `id` equals `5` and set the `user_id` field on product to that users id.

```json
{
  "data": {
    "name": "Nice Handbag",
    "description": "This is a really nice handbag",
    "price": 200,
    "user": {
      "connect": { "id": 5 }
    }
  }
}
```

```graphql
mutation {
  product(insert: $data) {
    id
    name
  }
}
```

## Update a product

This will update a product with `id = 12` and change it's `name`, `description` and `price`.

```json
{
  "data": {
    "name": "Not nice Handbag",
    "description": "This is not really that nice a bag",
    "price": 100
  }
}
```

```graphql
mutation {
  product(update: $data, where: { id: { eq: 12 } }) {
    id
    name
  }
}
```

## Create a user, product with tags and categories

There is so much going on here. We are creating a user, his product and assigning some tags
and categories. Tags here is a simple text array column while categories is a bigint array column
that we have configured to act as a foreign key to the categories tables.

Since array columns cannot be foreign keys in Postgres we have to add a simple config to Super Graph
to set this up.

```yaml
tables:
  - name: products
    columns:
      - name: category_ids
        related_to: categories.id
  ...
```

```json
{
  "data": {
    "email": "alien@antfarm.com",
    "full_name": "aliens",
    "created_at": "now",
    "updated_at": "now",
    "product": {
      "name": "Bug Spray",
      "tags": ["bad bugs", "be gone"],
      "categories": {
        "connect": { "id": 1 }
      },
      "created_at": "now",
      "updated_at": "now"
    }
  }
}
```

```graphql
mutation {
  user(insert: $data) {
    id
    product {
      id
      name
      tags
      category {
        id
        name
      }
    }
  }
}
```

## Realtime subscriptions

This is one of the coolest features of Super Graph. It is a highly scalable way to get updates from the database as it updates. Below we use subscriptions to fetch the latest `purchases` from the database.

```json
{
  "cursor": null
}
```

```graphql
subscription {
  purchases(first: 5, after: $cursor) {
    product {
      id
      name
    }
    customer {
      id
      email
    }
  }
}
```

## Production secrets management

We recommend you use [Mozilla SOPS](https://github.com/mozilla/sops) for secrets management. The sops binary
is installed on the Super Graph app docker image. To use SOPS you create a yaml file with your secrets like the one below. You then need a secret key to encrypt it with you're options are to go with Google Cloud KMS, Amazon KMS, Azure Key Vault, etc. In production SOPS will automatically fetch the key from your defined KMS, decrypt the secrets file and make the values available to Super Graph via enviroment variables.

1. Create the secrets file

```yml
SG_DATABASE_PASSWORD: postgres
SG_AUTH_JWT_SECRET: jwt_token_secret_key
SG_SECRET_KEY: generic_secret_ke
```

2. Login to your cloud (Google Example)

```console
gcloud auth login
gcloud auth application-default login
```

3. Encrypt the secrets with the key

```console
sops -e -i ./config/prod.secrets.yml
```
