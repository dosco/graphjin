---
id: why
title: Why use Super Graph?
sidebar_label: Why Super Graph
---

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
      picture: avatar
      full_name
    }
  }
  purchases(
    limit: 10
    order_by: { created_at: desc }
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
