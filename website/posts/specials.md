---
chapter: 6
title: Specials
description: Graph queries, Array columns, Remote joins, etc
---

# Specials

This is all the really cool special stuff that you'll need but databases don't support or is hard to build. For example you want to build an feed for your website or join your query with an API call to Stripe, etc

#### TOC

### Table relationships

<mark>The below configs are only needed in special cases</mark> such as when you don't use foreign keys or when you want to create a relationship between two tables where a foreign key is not defined or cannot be defined like with array columns.

For example in the example below a relationship is defined between the `tags` column on the `posts` table with the `slug` column on the `tags` table. This cannot be defined as using foreign keys since the `tags` column is of type array `text[]` and Postgres for one does not allow foreign keys with array columns.

> But with GraphJin this is easy :") :+1:

```yaml title="Config File"
tables:
  - name: posts
    columns:
      - name: tags
        related_to: tags.slug
```

#### JSON or Array columns

The ablity to have `JSON/JSONB` and `Array` columns is often considered in the top most useful features of Postgres. There are many cases where using an array or a json column saves space and reduces complexity in your app. The only issue with these columns is that your SQL queries can get harder to write and maintain.

GraphJin steps in here to help you by supporting these columns right out of the box. It allows you to work with these columns just like you would with tables. Joining data against or modifying array columns using the `connect` or `disconnect` keywords in mutations is fully supported. Another very useful feature is the ability to treat `json` or `binary json (jsonb)` columns as separate tables, even using them in nested queries joining against related tables. To replicate these features on your own will take a lot of complex SQL. Using GraphJin means you don't have to deal with any of this - it just works.

#### Array Columns

Configure a relationship between an array column `tag_ids` which contains integer ids for tags and the column `id` in the table `tags`.

```yaml title="Config File"
tables:
  - name: posts
    columns:
      - name: tag_ids
        related_to: tags.id
```

```graphql
query getPosts {
  posts {
    title
    tags {
      name
      image
    }
  }
}
```

#### JSON Columns

Configure a JSON column called `tag_count` in the table `products` into a separate table. This JSON column contains a json array of objects each with a tag id and a count of the number of times the tag was used. As a seperate table you can nest it into your GraphQL query and treat it like table using any of the standard features like `order_by`, `limit`, `where clauses`, etc.

The configuration below tells GraphJin to create a virtual table called `tag_count` using the column `tag_count` from the `products` table. And that this new table has two columns `tag_id` and `count` of the listed types and with the defined relationships.

```yaml title="Config File"
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
query getProducts {
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

### Building a feed

Normally two tables are connected together by creating a foreign key on one of the tables. But what if you wanted this table to connect to any number of other tables without updating the table schema. This is called `Polymorphic Associations` and was made popular by [Ruby-on-Rails](https://guides.rubyonrails.org/association_basics.html#polymorphic-associations).

<mark>
If you wanted to build a Facebook like feed you'd need a single table to reference so many things, photos, posts, birthdays, videos, updates, and a lot more...
</mark>

One use case for this can be in a `notifications` table where you want to link each row to the table the notification is about. For example a notification about a comment to a comment table or a notification about a like to the table for the blog post, etc.

To make the type of a relationaship queryable you'll have to add a virtual table to the table config like below. This will automatically add a polymorphic relationship on any table in your database that has the columns `subject_type` and `subject_id` where the former holds the name of the related table and the latter its `id`.

```sql title="Notifications table"
create table notifications (
  id            bigint,
  for_user_id   bigint references users,
  key           text,
  subject_type  text,
  subject_id    bigint
)
```

```yaml title="Config File"
tables:
  - name: subject
    type: polymorphic
    columns:
      - name: subject_id
        related_to: subject_type.id
```

```graphql
query getNotificationsFeed {
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

### Remote Joins

It often happens that after fetching some data from the DB we need to call another API to fetch some more data and all this combined into a single JSON response. For example along with a list of users you need their last 5 payments from Stripe. This requires you to query your DB for the users and Stripe for the payments. GraphJin handles all this for you also only the fields you requested from the Stripe API are returned.

<mark>
GraphJin is able fetch remote data and merge it with the DB response in a fast and efficient way. All of this without you having to write a line of code.
</mark>

For example you need to list the last 3 payments made by a user. You will first need to look up the user in the database and then call the Stripe API to fetch his last 3 payments. For this to work your user table in the db has a `customer_id` column that contains his Stripe customer ID.

Similiarly you could also fetch the users last tweet, lead info from Salesforce or whatever else you need. It's fine to mix up several different `remote joins` into a single GraphQL query.

#### Stripe Example

The configuration is self explanatory. A `payments` field has been added under the `customers` table. This field is added to the `remotes` subsection that defines fields associated with `customers` that are remote and not real database columns.

The `id` parameter maps a column from the `customers` table to the `$id` variable. In this case it maps `$id` to the `customer_id` column.

```yaml title="Config File"
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

Just include `payments` like you would any other GraphQL selector under the `customers` selector. GraphJin will call the configured API for you and stitch (merge) the JSON the API sends back with the JSON generated from the database query. GraphQL features like aliases and fields all work.

```graphql
query getCustomers {
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

> And voila here is the result. You get all of this advanced and honestly complex querying capability without writing a single line of code.

```json title="Remote Join Result"
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

### Full text search

Every app these days needs search. Enought his often means reaching for something heavy like Solr. While this will work why add complexity to your infrastructure when Postgres has really great and fast full text search built-in. And since it's part of **Postgres** and **MySQL** it's also available in GraphJin.

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

```json title="Search Result"
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
