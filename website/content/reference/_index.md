---
title: "Reference"
description: "Support matrix, operators, config map, troubleshooting, and test-backed examples."
nav_group: "reference"
weight: 6
---

Reference pages collect the durable details engineers need while implementing and operating GraphJin.

| Page | Reference material |
| --- | --- |
| [Database Support](/reference/database-support/) | Dialect matrix, SQL/Mongo differences, ordering rules, and feature boundaries. |
| [Config Reference](/reference/config-reference/) | Map from the site guides to `CONFIG.md`, source-mode docs, and tested config surfaces. |
| [Operators And Directives](/reference/operators-directives/) | Filter operators, geo operators, pagination args, aggregate fields, directives, and MCP syntax. |
| [Troubleshooting](/reference/troubleshooting/) | Debugging paths for ordering, saved queries, auth, MCP, OpenAPI, cursors, and dialect gaps. |
| [Test-Backed Examples](/reference/test-backed-examples/) | Curated feature examples tied to Go example tests and focused unit tests. |

When behavior seems ambiguous, prefer the test-backed examples and MCP syntax reference over prose. They are closer to the compiler and are easier to regression-test.
