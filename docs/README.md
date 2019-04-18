---
home: true
heroImage: /logo.svg
heroText: "SUPER GRAPH"
tagline: Get an instant GraphQL API for your Rails apps.
actionText: Get Started →
actionLink: /guide
features:
- title: Simple
  details: Easy config file, quick to deploy, No code needed. It just works.
- title: High Performance
  details: Converts your GraphQL query into a fast SQL one.
- title: Written in GO
  details: Go is a language created at Google to build secure and fast web services.
footer: MIT Licensed | Copyright © 2018-present Vikram Rangnekar
---

![Super Graph Web UI](/super-graph-web-ui.png "Super Graph Web UI for web developers")

Without writing a line of code get an instant high-performance GraphQL API for your Ruby-on-Rails app. Super Graph will automatically understand your apps database and expose a secure, fast and complete GraphQL API for it. Built in support for Rails authentication and JWT tokens.

## Try it out

```bash
# download super graph source
git clone https://github.com/dosco/super-graph.git

# setup the demo rails app & database
./demo setup

# run the demo
./demo run

# signin to the demo app (user1@demo.com / 123456)
open http://localhost:3000

# try the super graph web ui
open http://localhost:8080
```

::: warning DEMO REQUIREMENTS  
This demo requires `docker` you can either install it using `brew` or from the 
docker website [https://docs.docker.com/docker-for-mac/install/](https://docs.docker.com/docker-for-mac/install/)
:::

## Try out GraphQL

```graphql 
query { 
  users {
    id
    email
    picture : avatar
    products(limit: 2, where: { price: { gt: 10 } }) {
      id
      name
      description
    }
  }
}
```

## Why I built Super Graph?

Honestly, cause it was more fun than my real work. After working on several product though my career I found myself hating building CRUD APIs (Create, Update, Delete, List, Show). It was always the same thing figure out what the UI needs then build an endpoint for it, if related data is needed than join with another table. I didn't want to write that code anymore I wanted the computer to just do it.

I always liked GraphQL it sounded friendly, but it still required me to write all the same database query code. Sure the API was nicer but it took a lot of work sometime even more than a simple REST API would have. I wanted a GraphQL server that just worked the second you deployed it without having to write a line of code.

And so after a lot of coffee and some Avocado toasts __Super Graph was born, a GraphQL server that just works, is high performance and easy to deploy__. I hope you find it as useful as I do and there's a lot more coming so hit that :star: to stay in the loop.

## Say hello 

[twitter.com/dosco](https://twitter.com/dosco)