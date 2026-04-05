---
chapter: 11
title: FAQ
description: Frequently asked questions
---

# FAQ

### Is it production ready?

Yes, before its release into the NodeJS ecosystem it has been a popular library in the GO ecosystem used in several projects. GraphJin has a large library of integration tests that covers all its usecases with Postgres and MySQL and detection of data races. This test framework is run on every commit to ensure high code quality. It also earns an A+ in the GO [code quality report](https://goreportcard.com/report/github.com/dosco/graphjin/core/v3). It has minimum dependencies on other GO libraries and is Fuzz tests.

### Why not just use SQL?

GraphJin uses GraphQL as a high level language that compiles down to a single efficient SQL query. It's similar to you using Javascript a high level language instead of writing machine code. To achieve the same high quality SQL it would require your dev team to be very familiar with SQL and your DB schema including indexes. Also this handrolled SQL has to be maintained, tested, etc. Also keep in mind as new optimizations are added to GraphJin all your queries magically get better. Security is another reason to go with GraphJin which generates prepared statements in the database protects against SQL inject attacks as well as several other mitigations.

### What makes it fast?

No matter how complex your GraphQL query GraphJin will only generate a single query using knowledge of indexes, other parameters of your database and SQL best practices. This query is then converted into a prepared statement by the database query parser and planner. All requests for the query then go directly to the database prepared statement which is fast and secure. No inefficient query patterns like N+1 queries is possible.

### What are filters?

Query filters (`filter`) in the role table configuration are there to enforce table wide filters like `{ disabled: false }` which will cause disabled users (users with column disabled set to true) from never been shown in any query. Filters use the same GraphQL expressions used in the [`Where`](cheatsheet#crafting-the-where-clause) query

```yaml title="Config file"
roles:
  - name: user
    tables:
      - name: users
        query:
          filters: ["{ disabled: false }"]
```
