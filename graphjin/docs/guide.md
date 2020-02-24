---
sidebar: auto
---

# Guide to Super Graph

Super Graph is a service that instantly and without code gives you a high performance and secure GraphQL API. Your GraphQL queries are auto translated into a single fast SQL query. No more spending weeks or months writing backend API code. Just make the query you need and Super Graph will do the rest. 

Super Graph has a rich feature set like integrating with your existing Ruby on Rails apps, joining your DB with data from remote APIs, Role and Attribute based access control, Support for JWT tokens, DB migrations, seeding and a lot more.


## Features

- Role and Attribute based access control
- Works with existing Ruby-On-Rails apps
- Automatically learns database schemas and relationships
- Full text search and aggregations
- Rails authentication supported (Redis, Memcache, Cookie)
- JWT tokens supported (Auth0, etc)
- Join database with remote REST APIs
- Highly optimized and fast Postgres SQL queries
- GraphQL queries and mutations
- A simple config file
- High performance GO codebase
- Tiny docker image and low memory requirements
- Fuzz tested for security
- Database migrations tool
- Database seeding tool


## Try the demo app

```bash
# clone the repository
git clone https://github.com/dosco/super-graph

# run db in background
docker-compose up -d db

# see logs and wait until DB is really UP
docker-compose logs db

# setup the demo rails app & database and run it
docker-compose run rails_app rake db:create db:migrate db:seed

# run the demo
docker-compose up

# signin to the demo app (user1@demo.com / 123456)
open http://localhost:3000

# try the super graph web ui
open http://localhost:8080
```

::: tip DEMO REQUIREMENTS
This demo requires `docker` you can either install it using `brew` or from the
docker website [https://docs.docker.com/docker-for-mac/install/](https://docs.docker.com/docker-for-mac/install/)
:::

#### Trying out GraphQL

We fully support queries and mutations. For example the below GraphQL query would fetch two products that belong to the current user where the price is greater than 10.

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

::: tip Testing with a user
In development mode you can use the `X-User-ID: 4` header to set a user id so you don't have to worries about cookies etc. This can be set using the *HTTP Headers* tab at the bottom of the web UI.
:::

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

## Why Super Graph

Let's take a simple example say you want to fetch 5 products priced over 12 dollars along with the photos of the products and the users that owns them. Additionally also fetch the last 10 of your own purchases along with the name and ID of the product you purchased. This is a common type of query to render a view in say an ecommerce app. Lets be honest it's not very exciting write and maintain. Keep in mind the data needed will only continue to grow and change as your app evolves. Developers might find that most ORMs will not be able to do all of this in a single SQL query and will require n+1 queries to fetch all the data and assembly it into the right JSON response.

What if I told you Super Graph will fetch all this data with a single SQL query and without you having to write a single line of code. Also as your app evolves feel free to evolve the query as you like. In our experience Super Graph saves us hundreds or thousands of man hours that we can put towards the more exciting parts of our app.

#### GraphQL Query

```graphql
query {
    products(limit: 5, where: { price: { gt: 12 } }) {
      id
      name
      description
      price
      photos {
        url
      }
      user {
        id
        email
        picture : avatar
        full_name
      }
  }
  purchases(
      limit: 10, 
      order_by: { created_at: desc } , 
      where: { user_id: { eq: $user_id } }
    ) {
    id 
    created_at
    product {
      id
      name
    }
  }
}
```

#### JSON Result

```json

  "data": {
    "products": [
      {
        "id": 1,
        "name": "Oaked Arrogant Bastard Ale",
        "description": "Coors lite, European Amber Lager, Perle, 1272 - American Ale II, 38 IBU, 6.4%, 9.7°Blg",
        "price": 20,
        "photos: [{
          "url": "https://www.scienceworld.ca/wp-content/uploads/science-world-beer-flavours.jpg"
        }],
        "user": {
          "id": 1,
          "email": "user0@demo.com",
          "picture": "https://robohash.org/sitaliquamquaerat.png?size=300x300&set=set1",
          "full_name": "Mrs. Wilhemina Hilpert"
        }
      },
      ...
    ]
  },
  "purchases": [
    {
      "id": 5,
      "created_at": "2020-01-24T05:34:39.880599",
      "product": {
        "id": 45,
        "name": "Brooklyn Black",
      }
    },
    ...
  ]
}
```

## Get Started

Super Graph can generate your initial app for you. The generated app will have config files, database migrations and seed files among other things like docker related files.

You can then add your database schema to the migrations, maybe create some seed data using the seed script and launch Super Graph. You're now good to go and can start working on your UI frontend in React, Vue or whatever.

```bash
# Download and install Super Graph. You will need Go 1.13 or above
git clone https://github.com/dosco/super-graph && cd super-graph && make install
```

And then create and launch your new app

```bash
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
paragraph
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

Multiple tables can also be fetched using a single GraphQL query. This is very fast since the entire query is converted into a single SQL query which the database can efficiently run.

```graphql
query {
  user {
    full_name
    email
  }
  products {
    name
    description
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

### Sorting

To sort or ordering results just use the `order_by` argument. This can be combined with `where`, `search`, etc to build complex queries to fit you needs.

```graphql
query {
  products(order_by: { cached_votes_total: desc }) {
    id
    name
  }
}
```

### Filtering

Super Graph support complex queries where you can add filters, ordering,offsets and limits on the query. For example the below query will list all products where the price is greater than 10 and the id is not 5.

```graphql
query {
  products(where: { 
      and: { 
        price: { gt: 10 }, 
        not: { id: { eq: 5 } } 
      } 
    }) {
    name
    price
  }
}
```

#### Nested where clause targeting related tables

Sometimes you need to query a table based on a condition that applies to a related table. For example say you need to list all users who belong to an account. This query below will fetch the id and email or all users who belong to the account with id 3.

```graphql
query {
  users(where: { 
      accounts: { id: { eq: 3 } }
    }) {
    id
    email
  }
}`
```

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

In GraphQL mutations is the operation type for when you need to modify data. Super Graph supports the `insert`, `update`, `upsert` and `delete`. You can also do complex nested inserts and updates.

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

#### Bulk insert

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

#### Bulk update

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

#### Bulk delete

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

#### Bulk upsert 

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

Often you will need to create or update multiple related items at the same time. This can be done using nested mutations. For example you might need to create a product and assign it to a user, or create a user and his products at the same time. You just have to use simple json to define you mutation and Super Graph takes care of the rest.

### Nested Insert 

Create a product item first and then assign it to a user

```json
{
  "data": {
    "name": "Apple",
    "price": 1.25,
    "created_at": "now",
    "updated_at": "now",
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
    user {
      id
      full_name
      email
    }
  }
}
```

Or it's reverse, create the user first and then his product

```json
{
  "data": {
    "email": "thedude@rug.com",
    "full_name": "The Dude",
    "created_at": "now",
    "updated_at": "now",
    "product": {
      "name": "Apple",
      "price": 1.25,
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
    full_name
    email
    product {
      id
      name
      price
    }
  }
}
```

### Nested Update 

Update a product item first and then assign it to a user

```json
{
  "data": {
    "name": "Apple",
    "price": 1.25,
    "user": {
      "connect": { "id": 5 }
    }
  }
}
```

```graphql
mutation {
  product(update: $data, id: 5) {
    id
    name
    user {
      id
      full_name
      email
    }
  }
}
```

Or it's reverse, update a user first and then his product

```json
{
  "data": {
    "email": "newemail@me.com",
    "full_name": "The Dude",
    "product": {
      "name": "Banana",
      "price": 1.25,
    }
  }
}
```

```graphql
mutation {
  user(update: $data, id: 1) {
    id
    full_name
    email
    product {
      id
      name
      price
    }
  }
}
```

### Pagination

This is a must have feature of any API. When you want your users to go thought a list page by page or implement some fancy infinite scroll you're going to need pagination. There are two ways to paginate in Super Graph.

 Limit-Offset
This is simple enough but also inefficient when working with a large number of total items. Limit, limits the number of items fetched and offset is the point you want to fetch from. The below query will fetch 10 results at a time starting with the 100th item. You will have to keep updating offset (110, 120, 130, etc ) to walk thought the results so make offset a variable.

```graphql
query {
  products(limit: 10, offset: 100) {
    id
    slug
    name
  }
}
```

#### Cursor
This is a powerful and highly efficient way to paginate though a large number of results. Infact it does not matter how many total results there are this will always be lighting fast. You can use a cursor to walk forward of backward though the results. If you plan to implement infinite scroll this is the option you should choose. 

When going this route the results will contain a cursor value this is an encrypted string that you don't have to worry about just pass this back in to the next API call and you'll received the next set of results. The cursor value is encrypted since its contents should only matter to Super Graph and not the client. Also since the primary key is used for this feature it's possible you might not want to leak it's value to clients. 

You will need to set this config value to ensure the encrypted cursor data is secure. If not set a random value is used which will change with each deployment breaking older cursor values that clients might be using so best to set it.

```yaml
# Secret key for general encryption operations like 
# encrypting the cursor data
secret_key: supercalifajalistics
```

Paginating forward through your results

```json
{
  "variables": { 
    "cursor": "MJoTLbQF4l0GuoDsYmCrpjPeaaIlNpfm4uFU4PQ="
  }
}
```

```graphql
query {
  products(first: 10, after: $cursor) {
    slug
    name
  }
}
```

Paginating backward through your results

```graphql
query {
  products(last: 10, before: $cursor) {
    slug
    name
  }
}
```

```graphql
"data": {
   "products": [
    {
      "slug": "eius-nulla-et-8",
      "name" "Pale Ale"
    },
    {
      "slug": "sapiente-ut-alias-12",
      "name" "Brown Ale"
    }
    ...
  ],
  "products_cursor": "dJwHassm5+d82rGydH2xQnwNxJ1dcj4/cxkh5Cer"
}
```

Nested tables can also have cursors. Requesting multiple cursors are supported on a single request but when paginating using a cursor only one table is currently supported. To explain this better, you can only use a `before` or `after` argument with a cursor value to paginate a single table in a query.

```graphql
query {
  products(last: 10) {
    slug
    name
    customers(last: 5) {
      email
      full_name
    }
  }
}
```

Multiple order-by arguments are supported. Super Graph is smart enough to allow cursor based pagination when you also need complex sort order like below.

```graphql
query {
  products(
    last: 10
    before: $cursor
    order_by: [ price: desc, total_customers: asc ]) {
    slug
    name
  }
}
```


## Using Variables

Variables (`$product_id`) and their values (`"product_id": 5`) can be passed along side the GraphQL query. Using variables makes for better client side code as well as improved server side SQL query caching. The built-in web-ui also supports setting variables. Not having to manipulate your GraphQL query string to insert values into it makes for cleaner
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

## GraphQL with React

This is a quick simple example using `graphql.js` [https://github.com/f/graphql.js/](https://github.com/f/graphql.js/)

```js
import React, { useState, useEffect } from 'react'
import graphql from 'graphql.js'

// Create a GraphQL client pointing to Super Graph
var graph = graphql("http://localhost:3000/api/v1/graphql", { asJSON: true })

const App = () => {
  const [user, setUser] = useState(null)
  
  useEffect(() => {
    async function action() {
      // Use the GraphQL client to execute a graphQL query
      // The second argument to the client are the variables you need to pass
      const result = await graph(`{ user { id first_name last_name picture_url } }`)()
      setUser(result)
    }
    action()
  }, []);

  return (
    <div className="App">
      <h1>{ JSON.stringify(user) }</h1>
    </div>
  );
}
```

export default App;

## Advanced Columns

The ablity to have `JSON/JSONB` and `Array` columns is often considered in the top most useful features of Postgres. There are many cases where using an array or a json column saves space and reduces complexity in your app. The only issue with these columns is the really that your SQL queries can get harder to write and maintain. 

Super Graph steps in here to help you by supporting these columns right out of the box. It allows you to work with these columns just like you would with tables. Joining data against or modifying array columns using the `connect` or `disconnect` keywords in mutations is fully supported. Another very useful feature is the ability to treat `json` or `binary json (jsonb)` columns as seperate tables, even using them in nested queries joining against related tables. To replicate these features on your own will take a lot of complex SQL. Using Super Graph means you don't have to deal with any of this it just works.

### Array Columns

Configure a relationship between an array column `tag_ids` which contains integer id's for tags and the column `id` in the table `tags`.

```yaml
tables:
  - name: posts
    columns:
      - name: tag_ids
        related_to: tags.id

```

```graphql
query {
  posts {
    title
    tags {
      name
      image
    }
  }
}
```

### JSON Column

Configure a JSON column called `tag_count` in the table `products` into a seperate table. This JSON column contains a json array of objects each with a tag id and a count of the number of times the tag was used. As a seperate table you can nest it into your GraphQL query and treat it like table using any of the standard features like `order_by`, `limit`, `where clauses`, etc.

The configuration below tells Super Graph to create a synthetic table called `tag_count` using the column `tag_count` from the `products` table. And that this new table has two columns `tag_id` and `count` of the listed types and with the defined relationships.

```yaml
tables:
  - name: tag_count
    table: products
    columns:
      - name: tag_id
        type: bigint
        related_to: tags.id
      - name: count
        type: int
```

```graphql
query {
  products {
    name
    tag_counts {
      count
      tag {
        name
      }
    }
  }
}
```


## Full text search

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

## API Security

One of the the most common questions I get asked is what happens if a user out on the internet sends queries
that we don't want run. For example how do we stop him from fetching all users or the emails of users. Our answer to this is that it is not an issue as this cannot happen, let me explain.

Super Graph runs in one of two modes `development` or `production`, this is controlled via the config value `production: false` when it's false it's running in development mode and when true, production. In development mode all the **named** queries (including mutations) are saved to the allow list `./config/allow.list`. While in production mode when Super Graph starts only the queries from this allow list file are registered with the database as [prepared statements](https://stackoverflow.com/questions/8263371/how-can-prepared-statements-protect-from-sql-injection-attacks). 

Prepared statements are designed by databases to be fast and secure. They protect against all kinds of sql injection attacks and since they are pre-processed and pre-planned they are much faster to run then raw sql queries. Also there's no GraphQL to SQL compiling happening in production mode which makes your queries lighting fast as they are directly sent to the database with almost no overhead.

In short in production only queries listed in the allow list file `./config/allow.list` can be used, all other queries will be blocked. 

::: tip How to think about the allow list?
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

### JWT Tokens

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

For JWT tokens we currently support tokens from a provider like Auth0 or if you have a custom solution then we look for the `user_id` in the `subject` claim of of the `id token`. If you pick Auth0 then we derive two variables from the token `user_id` and `user_id_provider` for to use in your filters.

We can get the JWT token either from the `authorization` header where we expect it to be a `bearer` token or if `cookie` is specified then we look there.

For validation a `secret` or a public key (ecdsa or rsa) is required. When using public keys they have to be in a PEM format file.

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

It's common for APIs to control what information they return or insert based on the role of the user. In Super Graph we have two primary roles `user` and `anon` the first for users where a `user_id` is available the latter for users where it's not.

::: tip
An authenticated request is one where Super Graph can extract an `user_id` based on the configured authentication method (jwt, rails cookies, etc).
:::

The `user` role can be divided up into further roles based on attributes in the database. For example when fetching a list of users, a normal user can only fetch his own entry while an admin can fetch all the users within a company and an admin user can fetch everyone. In some places this is called Attribute based access control. So in way we support both. Role based access control and Attribute based access control.

Super Graph allows you to create roles dynamically using a `roles_query` and `  match` config values.

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
          columns: ["id", "name", "description" ]
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

## Database Relationships

In most cases you don't need this configuration, Super Graph will discover and learn
the relationship graph within your database automatically. It does this using `Foreign Key` relationships that you have defined in your database schema.

The below configs are only needed in special cases such as when you don't use foreign keys or when you want to create a relationship between two tables where a foreign key is not defined or cannot be defined.

For example in the sample below a relationship is defined between the `tags` column on the `posts` table with the `slug` column on the `tags` table. This cannot be defined as using foreign keys since the `tags` column is of type array `text[]` and Postgres for one does not allow foreign keys with array columns.

```yaml
tables:
  - name: posts
    columns:
      - name: tags
        related_to: tags.slug
```


## Configuration

Configuration files can either be in YAML or JSON their names are derived from the `GO_ENV` variable, for example `GO_ENV=prod` will cause the `prod.yaml` config file to be used. or `GO_ENV=dev` will use the `dev.yaml`. A path to look for the config files in can be specified using the `-path <folder>` command line argument.

We're tried to ensure that the config file is self documenting and easy to work with.

```yaml
# Inherit config from this other config file
# so I only need to overwrite some values
inherits: base

app_name: "Super Graph Development"
host_port: 0.0.0.0:8080
web_ui: true

# debug, info, warn, error, fatal, panic
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
  # the user_id via a header for testing. 
  # Disable in production
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

  # Set session variable "user.id" to the user id
  # Enable this if you need the user id in triggers, etc
  set_user_id: false

  # Define additional variables here to be used with filters
  variables:
    admin_account_id: "5"

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

roles_query: "SELECT * FROM users WHERE id = $user_id"

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
          disable_functions: false

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
          block: true

  - name: admin
    match: id = 1000
    tables:
      - name: users
        filters: []

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

## Developing Super Graph

If you want to build and run Super Graph from code then the below commands will build the web ui and launch Super Graph in developer mode with a watcher to rebuild on code changes. And the demo rails app is also launched to make it easier to test changes.

```bash

# yarn is needed to build the web ui
brew install yarn

# yarn install dependencies and build the web ui
(cd web && yarn install && yarn build)

# do this the only the time to setup the database
docker-compose run rails_app rake db:create db:migrate db:seed

# start super graph in development mode with a change watcher
docker-compose up

```

## Learn how the code works

[Super Graph codebase explained](https://supergraph.dev/internals.html)

## Apache License 2.0

Apache Public License 2.0 | Copyright © 2018-present Vikram Rangnekar
