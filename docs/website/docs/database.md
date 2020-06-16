---
id: database
title: Database made easy
sidebar_label: Database Config
---

## Database Relationships

In most cases Super Graph will discover and learn the relationship graph within your database automatically. It does this using `Foreign Key` relationships that you have defined in your database schema.

The below configs are only needed in special cases such as when you don't use foreign keys or when you want to create a relationship between two tables where a foreign key is not defined or cannot be defined.

For example in the sample below a relationship is defined between the `tags` column on the `posts` table with the `slug` column on the `tags` table. This cannot be defined as using foreign keys since the `tags` column is of type array `text[]` and Postgres for one does not allow foreign keys with array columns.

```yaml
tables:
  - name: posts
    columns:
      - name: tags
        related_to: tags.slug
```

## Polymorphic Relationships

Normally two tables are connected together by creating a foreign key on one of the tables. But what if you wanted
one table to connect to a union of tables? This is an association that frameworks like Ruby-on-Rails made popular https://guides.rubyonrails.org/association_basics.html#polymorphic-associations. Your database cannot help you here as foreign keys are only between two fixed tables.

One use case for this can be in a `notifications` table where you want to link each row to the table the notification is about. For example a notification about a comment to a comment table or a notification about a like to the table for the blog post, etc.

To make the type of a relationaship queryable you'll have to add a virtual table to the table config like below. This will automatically add a polymorphic relationship on any table in your database that has the columns `subject_type` and `subject_id` where the former holds the name of the related table and the latter its `id`.

Example notifications table

```sql
create table notifications (
  id            bigint,
  for_user_id   bigint references users,
  key           text,
  subject_type  text,
  subject_id    bigint
)
```

Example table config entry

```yaml
tables:
  - name: subject
    type: polymorphic
    columns:
      - name: subject_id
        related_to: subject_type.id
```

Query that uses this relationship

```graphql
query {
  notifications(limit: 10) {
    id
    key
    subjects {
      ... on comment {
        id
        message
      }
      ... on posts {
        id
        title
      }
    }
  }
}
```

## Advanced Columns

The ablity to have `JSON/JSONB` and `Array` columns is often considered in the top most useful features of Postgres. There are many cases where using an array or a json column saves space and reduces complexity in your app. The only issue with these columns is that your SQL queries can get harder to write and maintain.

Super Graph steps in here to help you by supporting these columns right out of the box. It allows you to work with these columns just like you would with tables. Joining data against or modifying array columns using the `connect` or `disconnect` keywords in mutations is fully supported. Another very useful feature is the ability to treat `json` or `binary json (jsonb)` columns as separate tables, even using them in nested queries joining against related tables. To replicate these features on your own will take a lot of complex SQL. Using Super Graph means you don't have to deal with any of this - it just works.

### Array Columns

Configure a relationship between an array column `tag_ids` which contains integer ids for tags and the column `id` in the table `tags`.

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

Configure a JSON column called `tag_count` in the table `products` into a separate table. This JSON column contains a json array of objects each with a tag id and a count of the number of times the tag was used. As a seperate table you can nest it into your GraphQL query and treat it like table using any of the standard features like `order_by`, `limit`, `where clauses`, etc.

The configuration below tells Super Graph to create a virtual table called `tag_count` using the column `tag_count` from the `products` table. And that this new table has two columns `tag_id` and `count` of the listed types and with the defined relationships.

```yaml
tables:
  - name: tag_count
    table: products
    type: jsonb
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

## Remote Joins

It often happens that after fetching some data from the DB we need to call another API to fetch some more data and all this combined into a single JSON response. For example along with a list of users you need their last 5 payments from Stripe. This requires you to query your DB for the users and Stripe for the payments. Super Graph handles all this for you also only the fields you requested from the Stripe API are returned.

:::info Is this fast?
Super Graph is able fetch remote data and merge it with the DB response in an efficient manner. Several optimizations such as parallel HTTP requests and a zero-allocation JSON merge algorithm makes this very fast. All of this without you having to write a line of code.
:::

For example you need to list the last 3 payments made by a user. You will first need to look up the user in the database and then call the Stripe API to fetch his last 3 payments. For this to work your user table in the db has a `customer_id` column that contains his Stripe customer ID.

Similiarly you could also fetch the users last tweet, lead info from Salesforce or whatever else you need. It's fine to mix up several different `remote joins` into a single GraphQL query.

### Stripe Example

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
    order_by: { search_rank: desc }
  ) {
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
