---
chapter: 3
title: Insert
description: Insert into a table, Multiple related tables, Bulk insert
---

# Insert

#### TOC

### Insert into a single table

> Insert a user into a table while setting their email and full name. And return the id email and full_name of this new user

```graphql
mutation createUser {
  users(insert: { email: $email, full_name: $full_name }) {
    id
    email
    full_name
  }
}
```

> Add validation to the above query. The email variable must have a valid email format and be between 1 and 100 characters long. Also a full name is required if the nameRequired variable is set to true

```graphql
mutation createUser {
  @constraint(variable: "email", format: "email", min: 1, max: 100)
  @constraint(variable: "full_name", requiredIf: { nameRequired: true } ) {
  users(insert: { email: $email, full_name: $full_name }) {
    id
    email
    full_name
  }
}
```

### Insert into multiple related tables

> Create a new purchase in the purchases table as well as a product in the products table and a customer in the customers table and ensure the product and customer is connected to the purchase

```graphql
mutation createPurchaseCustomerAndProduct {
  purchases(
    insert: {
      id: $purchase_id
      quantity: $quantity
      customer: {
        id: $customer_id
        email: $customer_email
        full_name: $customer_name
      }
      product: {
        id: $product_id
        name: $product_name
        description: $product_description
        price: $product_price
      }
    }
  ) {
    quantity
    customer {
      id
      full_name
      email
    }
    product {
      id
      name
      price
    }
  }
}
```

> Ordering of the nested tables does not matter GraphJin will figure out the right thing to do

```graphql title="Create product and owner"
mutation createProductAndOwner {
  products(
    insert: {
      id: $product_id
      name: $product_name
      description: $product_description
      price: $product_price
      owner: { id: $owner_id, full_name: $owner_name, email: $owner_email }
    }
  ) {
    id
    name
    price
  }
}
```

```graphql title="Create owner and product"
mutation createProductAndOwner {
  users(
    insert: {
      id: $owner_id
      full_name: $owner_name
      email: $owner_email
      product: {
        id: $product_id
        name: $product_name
        description: $product_description
        price: $product_price
      }
    }
  ) {
    id
    full_name
    email
  }
}
```

> Create a new product and connect the user with id 6 as its owner.

```graphql
mutation createProductAndSetOwner {
  products(
    insert: {
      id: 2005
      name: "Product 2005"
      description: "Description for product 2005"
      price: 2015.5
      tags: ["Tag 1", "Tag 2"]
      category_ids: [1, 2, 3, 4, 5]
      owner: { connect: { id: 6 } }
    }
  ) {
    id
    name
    owner {
      id
      full_name
      email
    }
  }
}
```

### Insert into a recursive table

> Say your building a commenting system and need to connect a comment thats
> a reply to the parent comment that was replied to. The `find` keyword can either be `children` or `parent` depending on which direction of the relationship you want to search in.

```graphql
mutation createReplyComment {
  comments(
    insert: {
      id: 5003
      body: "Comment body 5003"
      created_at: "now"
      comments: { find: "parent", connect: { id: 5 } }
    }
  ) {
    id
    body
  }
}
```

### Bulk insert

```json title="Query Variables"
{
  "data": [
    {
      "name": "Art of Computer Programming",
      "description": "The Art of Computer Programming (TAOCP) is a comprehensive monograph written by computer scientist Donald Knuth",
      "price": 30.5
    },
    {
      "name": "Compilers: Principles, Techniques, and Tools",
      "description": "Known to professors, students, and developers worldwide as the 'Dragon Book' is available in a new edition",
      "price": 93.74
    }
  ]
}
```

```graphql
mutation bulkCreateProducts {
  product(insert: $data) {
    id
    name
  }
}
```

