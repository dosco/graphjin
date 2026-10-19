---
title: "Database Config"
description: "Configure database connections, pools, TLS, supported database types, and table mapping."
nav_group: "configure"
doc_kind: "reference"
weight: 20
---

## Single database

```yaml
sources:
  - name: app
    kind: database
    type: postgres
    default: true
    connection_string: ${DATABASE_URL}
    schema: public
    max_open_conns: 25
    max_idle_conns: 5
    ping_timeout: 2s
```

## Supported database types

GraphJin has configuration examples for PostgreSQL, MySQL/MariaDB, SQLite, Oracle, SQL Server, MongoDB, Snowflake, Redshift, ClickHouse, and CodeSQL sources.

CodeSQL is not a `kind: database` source. Declare source-code indexes with `kind: code` so source capabilities, read-only behavior, and managed SQLite setup are applied correctly.

## Connection pools and TLS

Use pool options to bound database concurrency and TLS options for secure connections. Prefer environment variables for secrets and connection strings.

```yaml
sources:
  - name: warehouse
    kind: database
    type: snowflake
    connection_string: ${SNOWFLAKE_DSN}
    private_key_path: ${SNOWFLAKE_KEY_PATH}
    key_passphrase: ${SNOWFLAKE_KEY_PASSPHRASE}

  - name: sqlserver
    kind: database
    type: mssql
    connection_string: ${MSSQL_DSN}
    encrypt: true
    trust_server_certificate: false
```

## Table mapping

```yaml
tables:
  - name: users
    table: app_users
    source: app
    schema: public
    columns:
      - name: id
        type: integer
        primary: true
      - name: account_id
        type: uuid
        blocked: true
```

Table config is where you declare aliases, schema overrides, blocklists, column metadata, named order presets, and explicit relationships when discovery is not enough.

## Explicit relationships

Use relationship config when introspection cannot infer a foreign key or when a relation crosses database/source boundaries:

```yaml
relationships:
  - from: app.orders.customer_id
    to: crm.customers.id
    as: customer
```

For multi-database mode, root fields can be merged from several databases in one response, while nested database joins extract parent keys and replace the child placeholder query with a source-specific child query.

{{< verified by="TestMultiDBConfigInConfig" file="core/multidb_test.go" line="113" >}}
{{< verified by="TestParseDBConfig_SnowflakeConnectionString" file="serv/mcp_test.go" line="1847" >}}
