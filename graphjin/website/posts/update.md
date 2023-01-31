---
chapter: 4
title: Update
description: Update rows in a single table, Multiple related tables, Array columns, Upsert, JSON columns
---

# Update

#### TOC

### Update a single table

> Update product name and description by its id

```graphql
mutation updateProduct {
  products(
    id: $product_id
    update: { name: $product_name, description: $product_description }
  ) {
    id
    name
  }
}
```

### Update multiple tables

> Update users full name, connect product (id: 99) to this user and disconnect product (id: 100) from the same user. The user in addition to getting name change will now be the owner of product 99 and not of product 100

```graphql
mutation updateUserAndHisProducts {
  users(
    id: $id
    update: {
      id: 100
      data: {
        full_name: "Updated user 100"
        products: { connect: { id: 99 }, disconnect: { id: 100 } }
      }
    }
  ) {
    full_name
    products {
      id
    }
  }
}
```

> Update the users full name and also change the name of products he owns. The product(s) to change are identified using the `where` argument. All products that he owns with an id greater than 1 will now have an updated description.

```graphql
mutation updateUserAndHisProducts {
  users(id: $id, update: {
		id: 90,
		data: {
			full_name: "Updated user 90",
			products: {
				where: { id: { "gt": 1 } },
				description": "This product belongs to user 90"
			}
		}
	}) {
    full_name
    products {
      id
      description
    }
  }
}
```

### Update array columns

> Some databases like Postgres support the array column type. This is really useful to store short list saving you the overhead of having to create another table. We can quite easily update these array columns as well

```json title="Query Variables"
{ "tags": ["super", "great", "wow"] }
```

```graphql
mutation updateProductTags {
  products(where: { id: 100 }, update: { tags: $tags }) {
    id
    tags
  }
}
```

### Upsert

> Often you will find the need to update an existing record or create a new one. Using the upsert query makes this happen using a single atomic action. If a product with the id 1 is found then its updated else a new product is created.

```graphql
mutation updateOrInsertProduct {
  products(
    upsert: { name: "my_name", description: "my_desc" }
    where: { id: { eq: 1 } }
  ) {
    id
    name
  }
}
```

### Deleting

> Need I say more

```graphql
mutation deleteProduct {
  products(delete: true, where: { id: { eq: 1 } }) {
    id
    name
  }
}
```
