---
title: "Environment And Production"
description: "Use environment-specific config files, inheritance, environment variables, and production defaults."
nav_group: "configure"
doc_kind: "reference"
weight: 70
---

## Config selection

GraphJin uses environment-specific files:

| Environment | File |
| --- | --- |
| development | `dev.yml` |
| production | `prod.yml` |
| agentic | `agentic.yml` |

`GO_ENV=agentic` requires `agentic.yml`; agentic configs can inherit production settings.

```yaml
# config/agentic.yml
inherits: prod
mode: agentic

sources:
  - name: graphjin
    kind: graphjin
    catalog: true
    metadata: true
    access:
      roots:
        gj_catalog: authenticated
        gj_security: admin
        gj_runtime: admin
```

{{< verified by="TestReadInConfigAgenticCanInheritProd" file="serv/serv_test.go" line="56" >}}

## Production recommendations

| Setting | Development | Production |
| --- | --- | --- |
| `mode` | `dev` | `prod` |
| `web_ui` | `true` | `false` |
| `auth_fail_block` | `false` | `true` |
| `disable_allow_list` | `true` | `false` |
| `debug` | `true` | `false` |

{{< verified by="TestNormalizeMode" file="core/validate_test.go" line="71" >}}
{{< verified by="TestNewConfigCatalogEnabledAutoProduction" file="serv/config_test.go" line="26" >}}

## Production query model

Production should run reviewed saved queries and disable raw ad hoc operations unless a deployment deliberately opts out for a trusted environment.

```yaml
mode: prod
disable_production_security: false

mcp:
  allow_raw_queries: false
  allow_mutations: false
```

## Environment variables

Secrets and connection strings should come from environment variables, not checked-in config.

```yaml
sources:
  - name: app
    kind: database
    type: postgres
    default: true
    connection_string: ${DATABASE_URL}
admin_secret_key: ${GJ_ADMIN_SECRET_KEY}
```

When updating config through the control plane, plaintext secret updates require keystore support; runtime events and errors should redact secrets before they reach the agent-facing graph.

{{< verified by="TestHandleUpdateCurrentConfig_RejectsPlaintextSecretWithoutKeystoreKey" file="serv/mcp_config_transaction_test.go" line="186" >}}
{{< verified by="TestGraphQLControlPlaneConfigRuntimeEventsAreRedacted" file="serv/control_plane_graphql_test.go" line="2234" >}}
