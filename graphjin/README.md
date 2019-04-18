<a href="https://supergraph.dev"><img src="https://supergraph.dev/logo.svg" width="100" height="100" align="right" /></a>

# Super Graph - Instant GraphQL API for Rails

![MIT license](https://img.shields.io/github/license/dosco/super-graph.svg)
![Docker build](https://img.shields.io/docker/cloud/build/dosco/super-graph.svg)
![Cloud native](https://img.shields.io/badge/cloud--native-enabled-blue.svg)

Get an high-performance GraphQL API for your Rails app in seconds without writing a line of code. Super Graph will auto-learn your database structure and relationships. Built in support for Rails authentication and JWT tokens. 

![Super Graph Web UI](docs/.vuepress/public/super-graph-web-ui.png?raw=true "Super Graph Web UI for web developers")

## Why I built Super Graph?

Honestly, cause it was more fun than my real work. After working on several product though my career I found myself hating building CRUD APIs (Create, Update, Delete, List, Show). It was always the same thing figure out what the UI needs then build an endpoint for it, if related data is needed than join with another table. I didn't want to write that code anymore I wanted the computer to just do it.

I always liked GraphQL it sounded friendly, but it still required me to write all the same database query code. Sure the API was nicer but it took a lot of work sometime even more than a simple REST API would have. I wanted a GraphQL server that just worked the second you deployed it without having to write a line of code.

And so after a lot of coffee and some Avocado toasts __Super Graph was born, a GraphQL server that just works, is high performance and easy to deploy__. I hope you find it as useful as I do and there's a lot more coming so hit that :star: to stay in the loop.

## Features
- Works with Rails database schemas
- Automatically learns schemas and relationships
- Belongs-To, One-To-Many and Many-To-Many table relationships
- Full text search and Aggregations
- Rails Auth supported (Redis, Memcache, Cookie)
- JWT tokens supported (Auth0, etc)
- Highly optimized and fast Postgres SQL queries
- Configure with a simple config file
- High performance GO codebase
- Tiny docker image and low memory requirements

## Documentation

[supergraph.dev](https://supergraph.dev)

## Contact me

[twitter.com/dosco](https://twitter.com/dosco)

## License

[MIT](http://opensource.org/licenses/MIT)

Copyright (c) 2019-present Vikram Rangnekar


