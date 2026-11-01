---
type: Skill
name: graphjin-config
description: "Use when viewing or proposing changes to GraphJin sources, roles, access, security, runtime settings, and reload behavior."
---

# GraphJin Config

Use the source-config manager tools for GraphJin configuration requests.

## Workflow

1. Call `mcp_neko_source_config_manager_describe_source_graph` to identify
   the selected GraphJin data engine and its current sources.
2. Call `mcp_neko_source_config_manager_ask_graphjin_config_agent` with the
   user's configuration question or requested change.
3. For a view or explanation, answer with the redacted configuration-agent
   result and the relevant source-graph details.
4. For a database source registration, call
   `mcp_neko_source_config_manager_list_source_secret_names` and use a stored
   name as `secretRef`. When the required name is absent, direct the admin to
   Admin > Settings > GraphJin Config to add it.
5. For an API source, use the managed OpenAPI asset from the submitted card.
   When the admin provides a hosted HTTPS URL in chat, call
   `mcp_neko_source_config_manager_import_openapi_spec`. When they uploaded a
   file, read the asset ID from the submitted values. When the submitted card
   provides metadata only, call
   `mcp_neko_source_config_manager_list_openapi_specs` to resolve its managed
   asset ID.
6. For a local file source, use the `localFiles` manifest from the submitted
   card. For S3 or GCS, collect the bucket and applicable prefix, region,
   endpoint, public base URL, and presign TTL; GraphJin uses the deployment
   runtime identity for object-store credentials.
7. For a supported edit, call
   `mcp_neko_source_config_manager_request_source_config_change` with the
   confirmed fields.
8. Summarize the proposed change and its approval status.

A view or explanation succeeds when the response includes the redacted result
from `ask_graphjin_config_agent`. An edit succeeds when the response includes
the proposal result from `request_source_config_change`.

When a tool returns an error, report the error and the workflow step that needs
attention.

## Supported Edits

- `register_source`: provide `name`, one of the customer source kinds
  (`database`, `api`, or `file`), and the fields for that kind. Database uses
  connection metadata and a stored `secretRef`; API uses a managed OpenAPI
  `specAssetId`; local file uses the card's managed `localFiles` manifest; S3
  and GCS use an object-store bucket and deployment runtime identity. File
  sources receive authenticated read-only access.
  A suitable database default is `read: authenticated`, `write: blocked`, and
  `delete: blocked`.
- `set_source_access`: provide the source name and confirmed `read`, `write`,
  and `delete` access modes.
- `add_role`: provide the role name and the JWT match expression confirmed by
  the admin.
- `enable_api_writes`: provide the API source, the spec key, the operation ID,
  and the allowed roles. One proposal turns on governed writes for that
  operation. Database sources stay read-only.

Use an unused source name for registration. Source access patches the named
source, and role changes upsert by role name. A full replacement is a separate
configuration operation requiring the current configuration and explicit admin
intent.
