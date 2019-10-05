<a href="https://supergraph.dev"><img src="https://supergraph.dev/hologram.svg" width="100" height="100" align="right" /></a>

# Super Graph - Build web products faster. Instant GraphQL APIs for your apps

![MIT license](https://img.shields.io/github/license/dosco/super-graph.svg)
![Docker build](https://img.shields.io/docker/cloud/build/dosco/super-graph.svg)
![Cloud native](https://img.shields.io/badge/cloud--native-enabled-blue.svg)
[![Discord Chat](https://img.shields.io/discord/628796009539043348.svg)](https://discord.gg/6pSWCTZ)  

Get an instant high performance GraphQL API for Postgres. No code needed. GraphQL is automatically transformed into efficient database queries.

![GraphQL](docs/.vuepress/public/graphql.png?raw=true "")

## The story of Super Graph?

After working on several products through my career I find that we spend way too much time on building API backends. Most APIs also require constant updating, this costs real time and money.
            
It's always the same thing, figure out what the UI needs then build an endpoint for it. Most API code involves struggling with an ORM to query a database and mangle the data into a shape that the UI expects to see.

I didn't want to write this code anymore, I wanted the computer to do it. Enter GraphQL, to me it sounded great, but it still required me to write all the same database query code.

Having worked with compilers before I saw this as a compiler problem. Why not build a compiler that converts GraphQL to highly efficient SQL.

This compiler is what sits at the heart of Super Graph with layers of useful functionality around it like authentication, remote joins, rails integration, database migrations and everything else needed for you to build production ready apps with it.

## Features

- Works with Rails database schemas
- Automatically learns schemas and relationships
- Belongs-To, One-To-Many and Many-To-Many table relationships
- Full text search and Aggregations
- Rails Auth supported (Redis, Memcache, Cookie)
- JWT tokens supported (Auth0, etc)
- Join with remote REST APIs
- Highly optimized and fast Postgres SQL queries
- Support GraphQL queries and mutations
- Configure with a simple config file
- High performance GO codebase
- Tiny docker image and low memory requirements
- Database migrations tool
- Write database seeding scripts in Javascript

## Documentation

[supergraph.dev](https://supergraph.dev)

## Contact me

[twitter/dosco](https://twitter.com/dosco)

[chat/super-graph](https://discord.gg/6pSWCTZ)

## License

[MIT](http://opensource.org/licenses/MIT)

Copyright (c) 2019-present Vikram Rangnekar


