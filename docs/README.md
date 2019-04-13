---
home: true
heroImage: /hero.png
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

I have a Rails app that gets a bit of traffic. While planning to improve the UI using React or Vue I found that my current APIs didn't have what we needed. I'd have to add more controllers and ensure they are providing the right amount of data. This required designing new APIs and making sure they match what the webdevs need. While this is all to common work I was bored and there had to be a better way.

All my Rails controllers were esentially wrappers around database queries and its not exactly fun writing more of them.

I always liked GraphQL it made everything so simple. Web devs can use GraphQL to fetch exactly the data they need. There is one small issue however you still hasve to write a lot of the same database code.

I wanted a GraphQL server that just worked the second you deployed it without having to write a line of code.

And so after a lot of coffee and some avocado toasts Super Graph was born. An instant GraphQL API service that's high performance and easy to deploy. I hope you find it as useful as I do and there's a lot more coming so hit that :star: to stay in the loop.

## Say hello 

[twitter.com/dosco](https://twitter.com/dosco)