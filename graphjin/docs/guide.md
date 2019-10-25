---
sidebar: auto
---

# Guide to Super Graph

Get an instant high performance GraphQL API for Postgres. No code needed. GraphQL is automatically transformed into efficient database queries. Also Designed to integrate with your Rails apps.


## Features

- Works with Rails database schemas
- Automatically learns schemas and relationships
- Belongs-To, One-To-Many and Many-To-Many table relationships
- Full text search and Aggregations
- Rails Auth supported (Redis, Memcache, Cookie)
- JWT tokens supported (Auth0, etc)
- Join with remote REST APIs
- Highly optimized and fast Postgres SQL queries
- Support GraphQL queries and mutations
- Configure with a simple config file
- High performance GO codebase
- Tiny docker image and low memory requirements
- Database migrations tool
- Write database seeding scripts in Javascript

## Try the demo app

```bash
# download the Docker compose config for the demo
curl -L -o demo.yml https://bit.ly/2mq05lW

# setup the demo rails app & database and run it
docker-compose -f demo.yml run rails_app rake db:create db:migrate db:seed

# run the demo
docker-compose -f demo.yml up

# signin to the demo app (user1@demo.com / 123456)
open http://localhost:3000

# try the super graph web ui
open http://localhost:8080
```

::: warning DEMO REQUIREMENTS
This demo requires `docker` you can either install it using `brew` or from the
docker website [https://docs.docker.com/docker-for-mac/install/](https://docs.docker.com/docker-for-mac/install/)
:::

#### Trying out GraphQL

We currently fully support queries and mutations. Support for `subscriptions` is work in progress. For example the below GraphQL query would fetch two products that belong to the current user where the price is greater than 10.

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

In another example the below GraphQL mutation would insert a product into the database. The first part of the below example is the variable data and the second half is the GraphQL mutation. For mutations data has to always ben passed as a variable.

```json
{
  "data": { 
    "name": "Art of Computer Programming",
    "description": "The Art of Computer Programming (TAOCP) is a comprehensive monograph written by computer scientist Donald Knuth",
    "price": 30.5
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

## Get Started

Super Graph can generate your initial app for you. The generated app will have config files, database migrations and seed files among other things like docker related files.

You can then add your database schema to the migrations, maybe create some seed data using the seed script and launch Super Graph. You're now good to go and can start working on your UI frontend in React, Vue or whatever.

```bash
# use the below command to download and install Super Graph. You will need Go 1.13 or above
GO111MODULE=on go get -u github.com/dosco/super-graph

# create a new app and change to it's directory
super-graph new blog; cd blog

# setup the app database and seed it with fake data. Docker compose will start a Postgres database for your app
docker-compose run blog_api ./super-graph db:setup

# and finally launch Super Graph configured for your app
docker-compose up
```

Lets take a look at the files generated by Super Graph when you create a new app

```bash
super-graph new blog

> created 'blog'
> created 'blog/Dockerfile'
> created 'blog/docker-compose.yml'
> created 'blog/config'
> created 'blog/config/dev.yml'
> created 'blog/config/prod.yml'
> created 'blog/config/seed.js'
> created 'blog/config/migrations'
> created 'blog/config/migrations/100_init.sql'
> app 'blog' initialized
```

### Docker files

Docker Compose is a great way to run multiple services while developing on your desktop or laptop. In our case we need Postgres and Super Graph to both be running and the `docker-compose.yml` is configured to do just that. The Super Graph service is named after your app postfixed with `_api`. The Dockerfile can be used build a containr of your app for production deployment.

```bash
docker-compose run blog_api ./super-graph help
```

### Config files

All the config files needs to configure Super Graph for your app are contained in this folder for starters you have two `dev.yaml` and `prod.yaml`. When the `GO_ENV` environment variable is set to `development` then `dev.yaml` is used and the prod one when it's set to `production`. Stage and Test are the other two environment options, but you can set the `GO_ENV` to whatever you like (eg. `alpha-test`) and Super Graph will look for a yaml file with that name to load config from.

### Seed.js

Having data flowing through your API makes building your frontend UI so much easier. When creafting say a user profile wouldn't it be nice for the API to return a fake user with name, picture and all. This is why having the ability to seed your database is important. Seeding cn also be used in production to setup some initial users like the admins or to add an initial set of products to a ecommerce store.

Super Graph makes this easy by allowing you to write your seeding script in plan old Javascript. The below file that auto-generated for new apps uses our built-in functions `fake` and `graphql` to generate fake data and use GraphQL mutations to insert it into the database.

```javascript
// Example script to seed database

var users = [];

for (i = 0; i < 10; i++) {
  var data = {
    full_name: fake.name(),
    email: fake.email()
  }

  var res = graphql(" \
  mutation { \
    user(insert: $data) { \
      id \
    } \
  }", { data: data })

  users.push(res.user)
}
```

You can generate the following fake data for your seeding purposes. Below is the list of fake data functions supported by the built-in fake data library. For example `fake.image_url()` will generate a fake image url or `fake.shuffle_strings(['hello', 'world', 'cool'])` will generate a randomly shuffled version of that array of strings or `fake.rand_string(['hello', 'world', 'cool'])` will return a random string from the array provided.

```
// Person
person
name
name_prefix
name_suffix
first_name
last_name
gender
ssn
contact
email
phone
phone_formatted
username
password

// Address
address
city
country
country_abr
state
state_abr
status_code
street
street_name
street_number
street_prefix
street_suffix
zip
latitude
latitude_in_range
longitude
longitude_in_range

// Beer
beer_alcohol
beer_hop
beer_ibu
beer_blg
beer_malt
beer_name
beer_style
beer_yeast

// Cars
vehicle
vehicle_type
car_maker
car_model
fuel_type
transmission_gear_type

// Text
word
sentence
paragrph
question
quote

// Misc
generate
boolean
uuid

// Colors
color
hex_color
rgb_color
safe_color

// Internet
url
image_url
domain_name
domain_suffix
ipv4_address
ipv6_address
simple_status_code
http_method
user_agent
user_agent_firefox
user_agent_chrome
user_agent_opera
user_agent_safari

// Date / Time
date
date_range
nano_second
second
minute
hour
month
day
weekday
year
timezone
timezone_abv
timezone_full
timezone_offset

// Payment
price
credit_card
credit_card_cvv
credit_card_number
credit_card_number_luhn
credit_card_type
currency
currency_long
currency_short

// Company
bs
buzzword
company
company_suffix
job
job_description
job_level
job_title

// Hacker
hacker_abbreviation
hacker_adjective
hacker_ingverb
hacker_noun
hacker_phrase
hacker_verb

//Hipster
hipster_word
hipster_paragraph
hipster_sentence

// File
extension
mine_type

// Numbers
number
numerify
int8
int16
int32
int64
uint8
uint16
uint32
uint64
float32
float32_range
float64
float64_range
shuffle_ints
mac_address

//String
digit
letter
lexify
rand_string
shuffle_strings
numerify
```

### Migrations

Easy database migrations is the most important thing when building products backend by a relational database. We make it super easy to manage and migrate your database.

```bash
super-graph db:new create_users
> created migration 'config/migrations/101_create_users.sql'
```

Migrations in Super Graph are plain old Postgres SQL. Here's an example for the above migration.

```sql
-- Write your migrate up statements here

CREATE TABLE public.users (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  full_name  text,
  email      text UNIQUE NOT NULL CHECK (length(email) < 255),
  created_at timestamptz NOT NULL NOT NULL DEFAULT NOW(),
  updated_at timestamptz NOT NULL NOT NULL DEFAULT NOW()
);

---- create above / drop below ----

-- Write your down migrate statements here. If this migration is irreversible
-- then delete the separator line above.

DROP TABLE public.users
```

We would encourage you to leverage triggers to maintain consistancy of your data for example here are a couple triggers that you can add to you init migration and across your tables.

```sql
-- This trigger script will set the updated_at column everytime a row is updated
CREATE OR REPLACE FUNCTION trigger_set_updated_at()
RETURNS TRIGGER SET SCHEMA 'public' LANGUAGE 'plpgsql' AS $$
BEGIN
  new.updated_at = now();
  RETURN new;
END;
$$;

...

-- An exmple of adding this trigger to the users table
CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.users
  FOR EACH ROW EXECUTE PROCEDURE trigger_set_updated_at();
```

```sql
-- This trigger script will set the user_id column to the current
-- Super Graph user.id value everytime a row is created or updated
CREATE OR REPLACE FUNCTION trigger_set_user_id()
RETURNS TRIGGER SET SCHEMA 'public' LANGUAGE 'plpgsql' AS $$
BEGIN
  IF TG_OP = 'UPDATE' THEN
    new.user_id = old.user_id;
  ELSE
    new.user_id = current_setting('user.id')::int;
  END IF;

  RETURN new;
END;
$$;

...

-- An exmple of adding this trigger to the blog_posts table
CREATE TRIGGER set_user_id BEFORE INSERT OR UPDATE ON public.blog_posts
  FOR EACH ROW EXECUTE PROCEDURE trigger_set_user_id();

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
  products(search: "ale") {
    name
  }
}
```

### Advanced queries

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

### Aggregations

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

## Mutations

In GraphQL mutations is the operation type for when you need to modify data. Super Graph supports the `insert`, `update`, `upsert` and `delete` database operations. Here are some examples.

When using mutations the data must be passed as variables since Super Graphs compiles the query into an prepared statement in the database for maximum speed. Prepared statements are are functions in your code when called they accept arguments and your variables are passed in as those arguments.

### Insert

```json
{
  "data": { 
    "name": "Art of Computer Programming",
    "description": "The Art of Computer Programming (TAOCP) is a comprehensive monograph written by computer scientist Donald Knuth",
    "price": 30.5
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

### Bulk insert

```json
{
  "data": [{ 
    "name": "Art of Computer Programming",
    "description": "The Art of Computer Programming (TAOCP) is a comprehensive monograph written by computer scientist Donald Knuth",
    "price": 30.5
  },
  { 
    "name": "Compilers: Principles, Techniques, and Tools",
    "description": "Known to professors, students, and developers worldwide as the 'Dragon Book' is available in a new edition",
    "price": 93.74
  }]
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

### Update

```json
{
  "data": { 
    "price": 200.0
  },
  "product_id": 5
}
```

```graphql
mutation {
  product(update: $data, id: $product_id) {
    id
    name
  }
}
```

### Bulk update

```json
{
  "data": { 
    "price": 500.0
  },
  "gt_product_id": 450.0,
  "lt_product_id:": 550.0
}
```

```graphql
mutation {
  product(update: $data, where: { 
      price: { gt: $gt_product_id, lt: lt_product_id } 
  }) {
    id
    name
  }
}
```

### Delete

```json
{
  "data": { 
    "price": 500.0
  },
  "product_id": 5
}
```

```graphql
mutation {
  product(delete: true, id: $product_id) {
    id
    name
  }
}
```

### Bulk delete

```json
{
  "data": { 
    "price": 500.0
  }
}
```

```graphql
mutation {
  product(delete: true, where: { price: { eq: { 500.0 } } }) {
    id
    name
  }
}
```

### Upsert

```json
{
  "data": { 
    "id": 5,
    "name": "Art of Computer Programming",
    "description": "The Art of Computer Programming (TAOCP) is a comprehensive monograph written by computer scientist Donald Knuth",
    "price": 30.5
  }
}
```

```graphql
mutation {
  product(upsert: $data) {
    id
    name
  }
}
```

### Bulk upsert 

```json
{
  "data": [{ 
    "id": 5,
    "name": "Art of Computer Programming",
    "description": "The Art of Computer Programming (TAOCP) is a comprehensive monograph written by computer scientist Donald Knuth",
    "price": 30.5
  },
  { 
    "id": 6,
    "name": "Compilers: Principles, Techniques, and Tools",
    "description": "Known to professors, students, and developers worldwide as the 'Dragon Book' is available in a new edition",
    "price": 93.74
  }]
}
```

```graphql
mutation {
  product(upsert: $data) {
    id
    name
  }
}
```

### Using variables

Variables (`$product_id`) and their values (`"product_id": 5`) can be passed along side the GraphQL query. Using variables makes for better client side code as well as improved server side SQL query caching. The build-in web-ui also supports setting variables. Not having to manipulate your GraphQL query string to insert values into it makes for cleaner
and better client side code.

```javascript
// Define the request object keeping the query and the variables seperate
var req = { 
  query: '{ product(id: $product_id) { name } }' ,
  variables: { "product_id": 5 }
}

// Use the fetch api to make the query
fetch('http://localhost:8080/api/v1/graphql', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(req),
})
.then(res => res.json())
.then(res => console.log(res.data));
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

## Remote Joins

It often happens that after fetching some data from the DB we need to call another API to fetch some more data and all this combined into a single JSON response. For example along with a list of users you need their last 5 payments from Stripe. This requires you to query your DB for the users and Stripe for the payments. Super Graph handles all this for you also only the fields you requested from the Stripe API are returned. 

::: tip Is this fast?
Super Graph is able fetch remote data and merge it with the DB response in an efficient manner. Several optimizations such as parallel HTTP requests and a zero-allocation JSON merge algorithm makes this very fast. All of this without you having to write a line of code.
:::

For example you need to list the last 3 payments made by a user. You will first need to look up the user in the database and then call the Stripe API to fetch his last 3 payments. For this to work your user table in the db has a `customer_id` column that contains his Stripe customer ID.

Similiarly you could also fetch the users last tweet, lead info from Salesforce or whatever else you need. It's fine to mix up several different `remote joins` into a single GraphQL query.

### Stripe API example

The configuration is self explanatory. A `payments` field has been added under the `customers` table. This field is added to the `remotes` subsection that defines fields associated with `customers` that are remote and not real database columns.

The `id` parameter maps a column from the `customers` table to the `$id` variable. In this case it maps `$id` to the `customer_id` column.

```yaml
tables:
  - name: customers
    remotes:
      - name: payments
        id: stripe_id
        url: http://rails_app:3000/stripe/$id
        path: data
        # debug: true
        # pass_headers: 
        #   - cookie
        #   - host
        set_headers:
          - name: Authorization
            value: Bearer <stripe_api_key>
```

#### How do I make use of this?

Just include `payments` like you would any other GraphQL selector under the `customers` selector. Super Graph will call the configured API for you and stitch (merge) the JSON the API sends back with the JSON generated from the database query. GraphQL features like aliases and fields all work.

```graphql
query {
  customers {
    id
    email
    payments {
      customer_id
      amount
      billing_details
    }
  }
}
```

And voila here is the result. You get all of this advanced and honestly complex querying capability without writing a single line of code.

```json
"data": {
  "customers": [
    {
      "id": 1,
      "email": "linseymertz@reilly.co",
      "payments": [
        {
          "customer_id": "cus_YCj3ndB5Mz",
          "amount": 100,
            "billing_details": {
            "address": "1 Infinity Drive",
            "zipcode": "94024"
          }
        },
      ...
```

Even tracing data is availble in the Super Graph web UI if tracing is enabled in the config. By default it is enabled in development. Additionally there you can set `debug: true` to enable http request / response dumping to help with debugging.

![Query Tracing](/tracing.png "Super Graph Web UI Query Tracing")

## Authentication

You can only have one type of auth enabled. You can either pick Rails or JWT. 

### Rails Auth (Devise / Warden)

Almost all Rails apps use Devise or Warden for authentication. Once the user is
authenticated a session is created with the users ID. The session can either be
stored in the users browser as a cookie, memcache or redis. If memcache or redis is used then a cookie is set in the users browser with just the session id.

Super Graph can handle all these variations including the old and new session formats. Just enable the right `auth` config based on how your rails app is configured.

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

### JWT Token Auth

```yaml
auth:
  type: jwt

  jwt:
    # the two providers are 'auth0' and 'none'
    provider: auth0 
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
app_name: "Super Graph Development"
host_port: 0.0.0.0:8080
web_ui: true

# debug, info, warn, error, fatal, panic
log_level: "debug"

# Disable this in development to get a list of 
# queries used. When enabled super graph
# will only allow queries from this list
# List saved to ./config/allow.list
use_allow_list: false

# Throw a 401 on auth failure for queries that need auth
# valid values: always, per_query, never
auth_fail_block: never

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
migrations_path: ./config/migrations

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
  creds_in_header: true

  rails:
    # Rails version this is used for reading the
    # various cookies formats.
    version: 5.2

    # Found in 'Rails.application.config.secret_key_base'
    secret_key_base: 0a248500a64c01184edb4d7ad3a805488f8097ac761b76aaa6c17c01dcb7af03a2f18ba61b2868134b9c7b79a122bc0dadff4367414a2d173297bfea92be5566

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

database:
  type: postgres
  host: db
  port: 5432
  dbname: app_development
  user: postgres
  password: ''

  #schema: "public"
  #pool_size: 10
  #max_retries: 0
  #log_level: "debug"

  # Define variables here that you want to use in filters
  # sub-queries must be wrapped in ()
  variables:
    account_id: "(select account_id from users where id = $user_id)"

  # Define defaults to for the field key and values below
  defaults:
    # filters: ["{ user_id: { eq: $user_id } }"]

    # Field and table names that you wish to block
    blocklist:
      - ar_internal_metadata
      - schema_migrations
      - secret
      - password
      - encrypted
      - token

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

roles_query: "SELECT * FROM users as usr WHERE id = $user_id"

roles:
  - name: anon
    tables:
      - name: products
        limit: 10

        query:
          columns: ["id", "name", "description" ]
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
          columns: ["id", "name", "description" ]
          disable_aggregation: false

        insert:
          filters: ["{ user_id: { eq: $user_id } }"]
          columns: ["id", "name", "description" ]
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
          deny: true

  - name: admin
    match: id = 1
    tables:
      - name: users
        # query:
        #   filters: ["{ account_id: { _eq: $account_id } }"]

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
docker-compose run rails_app rake db:create db:migrate db:seed

# start super graph in development mode with a change watcher
docker-compose up

```

## MIT License

MIT Licensed | Copyright © 2018-present Vikram Rangnekar
