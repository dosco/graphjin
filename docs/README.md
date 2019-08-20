---
layout: HomeLayout

home: true
heroText: "SUPER GRAPH"
heroImage: /super-graph-web-ui-half.png
heroImageMobile: /super-graph-web-ui.png
tagline: An instant high-performance GraphQL API. No code needed.
actionText: Get Started →
actionLink: /guide
features:
- title: Simple
  details: Easy config file, quick to deploy, No code needed. It just works.
- title: High Performance
  details: Compiles your GraphQL into a fast SQL query in realtime.
- title: Ruby-on-Rails
  details: Can read Rails cookies and supports rails database conventions.
- title: Serverless
  details: Designed for App Engine, Kubernetes, CloudRun, Horeku, AWS Fargate, etc  
- title: Go Lang
  details: Go is a language created at Google to build fast and secure web services. 
- title: Free and Open Source
  details: Not a VC funded startup. Not even a startup just good old open source code
footer: MIT Licensed | Copyright © 2018-present Vikram Rangnekar
---

## Try the demo

```bash
# download super graph source
git clone https://github.com/dosco/super-graph.git

# setup the demo rails app & database and run it
./demo start

# signin to the demo app (user1@demo.com / 123456)
open http://localhost:3000

# try the super graph web ui
open http://localhost:8080
```

## Try a query

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

## Why Super Graph?

After working on several product though my career I found building CRUD APIs (Create, Update, Delete, List, Show) to be a big part of the job. It was always the same thing figure out what the UI needs then build an endpoint for it, if related data is needed than join with another table. I didn't want to write that code anymore I wanted the computer to just do it.

I always liked GraphQL it sounded friendly, but it still required me to write all the same database query code. Sure the API was nicer but it took a lot of work sometime even more than a simple REST API would have. I wanted a GraphQL server that just worked the second you deployed it without having to write a line of code.

And so after a lot of coffee and some Avocado toasts __Super Graph was born, a GraphQL server that just works, is high performance and easy to deploy__. I hope you find it as useful as I do and there's a lot more coming so hit that :star: to stay in the loop.


## Say hello

[twitter.com/dosco](https://twitter.com/dosco)
