<!-- <a href="https://supergraph.dev"><img src="https://supergraph.dev/hologram.svg" width="100" height="100" align="right" /></a> -->

<img src="docs/.vuepress/public/super-graph.png" width="250" />

### Build web products faster. Secure high performance GraphQL

![Apache Public License 2.0](https://img.shields.io/github/license/dosco/super-graph.svg)
![Docker build](https://img.shields.io/docker/cloud/build/dosco/super-graph.svg)
![Cloud native](https://img.shields.io/badge/cloud--native-enabled-blue.svg)
[![Discord Chat](https://img.shields.io/discord/628796009539043348.svg)](https://discord.gg/6pSWCTZ)  


## What is Super Graph

Super Graph is a micro-service that instantly and without code gives you a high performance and secure GraphQL API. Your GraphQL queries are auto translated into a single fast SQL query. No more writing API code as you develop your web frontend just make the query you need and Super Graph will do the rest.

Super Graph has a rich feature set like integrating with your existing Ruby on Rails apps, joining your DB with data from remote APIs, Role and Attribute based access control, Supoport for JWT tokens, Built-in DB mutations and seeding and a lot more.

![GraphQL](docs/.vuepress/public/graphql.png?raw=true "")


## The story of Super Graph?

After working on several products through my career I find that we spend way too much time on building API backends. Most APIs also require constant updating, this costs real time and money.
            
It's always the same thing, figure out what the UI needs then build an endpoint for it. Most API code involves struggling with an ORM to query a database and mangle the data into a shape that the UI expects to see.

I didn't want to write this code anymore, I wanted the computer to do it. Enter GraphQL, to me it sounded great, but it still required me to write all the same database query code.

Having worked with compilers before I saw this as a compiler problem. Why not build a compiler that converts GraphQL to highly efficient SQL.

This compiler is what sits at the heart of Super Graph with layers of useful functionality around it like authentication, remote joins, rails integration, database migrations and everything else needed for you to build production ready apps with it.

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

## Get started

```
git clone https://github.com/dosco/super-graph 
cd ./super-graph
make install

super-graph new <app_name>
```

## Documentation

[supergraph.dev](https://supergraph.dev)

## Contact me

I'm happy to help you deploy Super Graph so feel free to reach out over
Twitter or Discord.

[twitter/dosco](https://twitter.com/dosco)

[chat/super-graph](https://discord.gg/6pSWCTZ)

## License

[Apache Public License 2.0](https://opensource.org/licenses/Apache-2.0)

Copyright (c) 2019-present Vikram Rangnekar


