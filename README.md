# Super Graph

## Instant GraphQL API for Rails. Zero code.

Get an high-performance GraphQL API for your Rails app in seconds. Super Graph will auto-learn your database structure and relationships. Built in support for Rails authentication and JWT tokens.

![Super Graph Web UI](docs/.vuepress/public/super-graph-web-ui.png?raw=true "Super Graph Web UI for web developers")

## Why I built Super Graph?

I have a Rails app that gets a bit of traffic. While planning to improve the UI using React or Vue I found that my current APIs didn't have what we needed. I'd have to add more controllers and ensure they are providing the right amount of data. This required designing new APIs and making sure they match what the webdevs need. While this is all to common work I was bored and there had to be a better way.

All my Rails controllers were esentially wrappers around database queries and its not exactly fun writing more of them.

I always liked GraphQL it made everything so simple. Web devs can use GraphQL to fetch exactly the data they need. There is one small issue however you still hasve to write a lot of the same database code.

I wanted a GraphQL server that just worked the second you deployed it without having to write a line of code.

And so after a lot of coffee and some avocado toasts Super Graph was born. An instant GraphQL API service that's high performance and easy to deploy. I hope you find it as useful as I do and there's a lot more coming so hit that :star: to stay in the loop.

## Features
- Support for Rails database conventions
- Belongs-To, One-To-Many and Many-To-Many table relationships
- Devise, Warden encrypted and signed session cookies
- Redis, Memcache and Cookie session stores
- JWT tokens supported from providers like Auth0
- Generates highly optimized and fast Postgres SQL queries
- Customize through a simple config file
- High performance GO codebase
- Tiny docker image and low memory requirements

## Contact me

[twitter.com/dosco](https://twitter.com/dosco)

## License

[MIT](http://opensource.org/licenses/MIT)

Copyright (c) 2019-present Vikram Rangnekar


