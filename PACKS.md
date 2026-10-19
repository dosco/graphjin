# Authoring solution packs

A solution pack bundles sources, API specifications, saved queries, metrics,
workflows, watchers, actions, policies, and skills for an application or business
use case. OpenNeko installs these artifacts through the shared pack lifecycle.
Packs that need connectors ship their own declarations and specifications; they
do not install runtime plugins.

For operator instructions, see [installing and upgrading custom packs](docs/CUSTOM_PACKS.md).
For contributing code, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Start from an example

- [Service health](apps/worker/test/fixtures/service-health/pack.yaml) is a small
  custom pack with an API connector, query, metric, workflow, watcher, and skill.
- [Magento](packs/magento/pack.yaml) is the first-party commerce pack. Its
  [README](packs/magento/README.md) explains application-specific prerequisites.
- [Google Workspace](packs/google-workspace/pack.yaml) shows customer-owned
  per-user OAuth, REST reads, and generic governed API actions.
- [Resend](packs/resend/pack.yaml), [Slack](packs/slack/pack.yaml),
  [Shopify](packs/shopify/pack.yaml), and
  [Parallel web search](packs/parallel-search/pack.yaml) replace the former
  action plugins with API-key sources. Resend shows all three
  [action safety fields](#action-safety-fields).

Use the small example for read-only custom packs. Use Google Workspace as the
reference for generic REST writes. Magento uses additional commerce-specific
action contracts for change sets and reconciliation.

## Directory layout

The manifest declares artifact paths, so directory names below are conventions,
not implicit discovery rules. All declared paths must exist and stay within the
pack root. Omit artifact paths for types the pack does not use. A declared empty
directory must still exist in the archive.

For a skills-only pack, declare `artifacts: { skills: [skills/my-skill] }`. Omit
`artifacts.graphjin` and `compatibility.graphjin`; `compatibility.applications`
and `compatibility.databases` can be empty arrays. Use empty health-check arrays
and an empty readiness map. This pack needs no data connection or GraphJin
configuration. GraphJin artifacts require GraphJin compatibility declarations.

To remove all GraphJin artifacts from an installed pack, uninstall it first.
This stops access through its owned sources before a new version is installed.

```text
my-pack/
├── pack.yaml
├── README.md
├── graphjin/
│   ├── sources.yaml
│   ├── relationships.yaml
│   ├── specs/
│   │   └── my-api.yaml
│   └── queries/
│       └── health.gql
├── metrics/
│   └── health.yaml
├── workflows/
│   └── review.yaml
├── watchers/
│   └── unhealthy.yaml
├── actions/
├── policies/
└── skills/
    └── my-pack-review/
        └── SKILL.md
```

YAML artifact directories accept `.yaml` and `.yml`; saved queries accept `.gql`
and `.graphql`. Each skill has an explicitly listed directory and `SKILL.md`.

## Manifest

Copy a working `pack.yaml` and edit these sections:

| Section | What to declare |
| --- | --- |
| `apiVersion`, `kind` | `openneko.app/v1` and `SolutionPack` |
| `metadata` | Stable lowercase-slug `id`, display `name`, semantic `version`, lowercase-slug `publisher` and `category` |
| `compatibility` | OpenNeko and GraphJin versions, supported application editions/versions, and database engines/versions |
| `inputs` | Named settings with a type: `string`, `url`, `integer`, `enum`, `timezone`, or `boolean`; defaults and required values as appropriate |
| `secrets` | Secret keys, purpose, and whether required; never credential values |
| `oauth` | Connection scope, provider label, setup/help copy, OAuth endpoints, input and secret references, account identity fields, and consent scopes |
| `management` | Optional label and local path to an existing application management page |
| `permissions.network` | Every host used by OAuth and the pack's API sources |
| `artifacts` | Source/relationship files, spec paths, query and YAML directories, and skill directories |
| `health` | Required preflight checks, readiness groups, post-install steps, and post-write canaries |

Custom inputs should have defaults or be supplied by the installer. Do not assume
Magento's application-specific discovery runs for a new pack. Declaring a health
check name also does not implement a new runtime check.

The [manifest schema](packages/packs/src/manifest.ts) is the authoritative field
reference. Unknown manifest fields are rejected.

## Artifacts and stable identities

Structured metrics, workflows, watchers, actions, and policies declare `key` and
`targetRef`. For example, a metric can use `key: metric.health` and
`targetRef: my-pack.healthy`. Keep identities stable across versions: the installer
uses them to track ownership and changes. Namespace installed targets to avoid
collisions with other packs or operator-created artifacts.

| Artifact | Authoring guidance |
| --- | --- |
| Sources | Declare API connectors or references to administrator-owned database sources in `sources.yaml`. |
| Relationships | Declare GraphJin relationships in the manifest's relationship file; use the example's empty declaration when none are needed. |
| OpenAPI specs | Describe the provider's actual paths, HTTP methods, parameters, bodies, and responses. Use local references. |
| Saved queries | Give queries stable filenames. Metric execution refers to the query name without its extension. |
| Metrics | Define presentation, cadence, source, saved query, result extraction, and freshness. See the [example metric](apps/worker/test/fixtures/service-health/metrics/health.yaml). |
| Workflows | Define a goal, output contract, and optional schedule with a timezone input. A workflow may declare `networkHosts` for read-only external requests; OpenShell scopes those hosts to that workflow sandbox. See the [example workflow](apps/worker/test/fixtures/service-health/workflows/health.yaml). |
| Watchers | Reference a workflow artifact key and define a query, value path, threshold, cadence, debounce, cooldown, and severity. |
| Skills | Write Markdown instructions with `name` and `description` frontmatter. Explain data sources, expected outputs, and permitted actions. |
| Actions and policies | Use `graphjin_api_operation` for reviewed REST writes. Group named operations by action kind and add a matching policy. Write policies install disabled. See [Action safety fields](#action-safety-fields). |

### Action safety fields

A `graphjin_api_operation` action can declare three checks:

- `targets`: payload fields that hold the action's targets, as dot paths such
  as `body.to`. With `as: email_domain` the policy engine reads each
  address's domain, so a policy's `allowed_targets` can limit recipients.
- `adapter.lockedBody`: body fields the agent cannot choose. Each one is set
  from a pack input and replaces any value in the payload, for example
  `from: { input: from_address }`.
- `idempotencyHeader` on an operation: a header that the API spec declares,
  such as `Idempotency-Key`. The runtime sends the action request id in it,
  so a retried request cannot act twice.

```yaml
targets:
  fields: [body.to, body.cc, body.bcc]
  as: email_domain
adapter:
  kind: graphjin_api_operation
  lockedBody:
    from: { input: from_address }
  operations:
    send:
      operationId: sendEmail
      mutationRoot: resend_send_email
      idempotencyHeader: Idempotency-Key
```

See the [artifact schemas](packages/packs/src/artifact-schema.ts) for exact fields
and the [bundle loader](packages/packs/src/bundle.ts) for identity and reference
validation. Avoid renaming a query file or skill casually: filenames and skill
names participate in artifact identity.

## Connectors and credentials

A custom API source can declare bearer authentication like this:

```yaml
sources:
  - name: my_pack_api
    kind: api
    base_url: "{{my-pack.base_url}}"
    openapi: graphjin/specs/my-api.yaml
    auth:
      type: bearer
      token: "{{secret.my-pack.api_token}}"
```

For a provider that expects an API key, send it in a named header or query
parameter. `key_in` accepts `header` (the default) or `query`. Prefer a header,
because a query value can appear in provider and proxy logs:

```yaml
    auth:
      type: api_key
      key_name: Api-Key
      key_in: header
      key_value: "{{secret.my-pack.api_token}}"
```

Declare `my-pack.base_url` in `inputs` and `my-pack.api_token` in `secrets`, with
purpose `graphjin_api_auth`. The installer supplies configuration and resolves
secret references through the encrypted secret store. Keep credentials out of
Git, archives, skills, and example payloads.

For OAuth, declare the connection and its credential references as described
below. Packs do not implement callback handlers, token storage, or refresh code.

API sources may request write or delete access. Each operation still needs a
pack action and policy. Write policies install disabled, so installing a pack
does not enable mutations. Packs do not use executable connectors for OAuth.

A generic REST action names its API source, OpenAPI spec, approved operation IDs,
and stable GraphQL mutation roots. At execution, the action accepts only a named
declared operation plus `path`, `query`, and `body` objects. OpenNeko sends that
complete reviewed payload through a short-lived `pack_api_executor` role. Read
operations are never exposed as mutations. See the
[Google Workspace actions](packs/google-workspace/actions) for complete examples.

Database source references require an existing read-only source binding and an
organization GraphJin data source. See the [installation guide](docs/CUSTOM_PACKS.md)
for `--source-id`, `--bind`, and secret-reference options.

## Installation and configuration experience

OpenNeko renders pack settings from `inputs`, `secrets`, and `oauth`; a new
provider using these supported declarations needs no provider-specific settings
component. Give inputs useful `description` values and declare defaults and enum
choices where appropriate. Keep IDs and credential reference keys stable across
versions.

Built-in packs already ship with OpenNeko. Administrators select **Install** in
**Admin → Settings → Packs**, without uploading an archive. This records the pack
definition first; it does not activate external sources or install runtime
artifacts. The installed pack then exposes its declared configuration. Reviewing
and applying that configuration activates the pack through the shared lifecycle.
In particular, an administrator cannot configure a personal OAuth client for an
uninstalled pack. Custom packs retain the upload, review, and install flow.

### Install into any GraphJin

OpenNeko installs a pack into the GraphJin of the selected data connection,
including a GraphJin that the customer runs outside OpenNeko. It sends the pack's
sources, OpenAPI documents, saved queries, and credentials through that GraphJin's
`gj_config` API, and GraphJin writes them to its own config folder. That
GraphJin needs:

- `production: false`, so it keeps changes made through its API. A production
  GraphJin keeps them only when OpenNeko manages its config folder, as with the
  GraphJin that ships with OpenNeko.
- `config.read` and `config.write` capabilities, with `gj_config` open to
  OpenNeko.
- `secrets.keystore.key` set, for a pack that brings a credential. GraphJin seals
  the credential with it.
- GraphJin 3.21.0 or later, which accepts inline OpenAPI documents and
  `update_saved_queries`.

When that GraphJin has no auth, every caller is anonymous. OpenNeko then makes
pack API sources readable by anyone who can reach GraphJin, and keeps them
read-only: GraphJin does not grant writes to an anonymous caller.

For an optional operations link, declare, for example:

```yaml
management:
  label: Manage store operations
  path: /admin/settings/packs/magento/operations
```

The link appears after installation and configuration. It must point to an
existing local application route; declaring it does not create a page. Paths
accept letters, digits, `/`, `_`, and `-`, and cannot be external URLs. Ordinary
pack configuration needs no management route. Packs supply supported fields and
copy; OpenNeko supplies shared controls, layout, accessibility, and account states.
Packs cannot ship arbitrary UI components, HTML, or CSS.

## Personal and deployment OAuth

Choose the connection scope explicitly:

| `oauth[].scope` | Account ownership and connection experience |
| --- | --- |
| `user` | An administrator saves the shared OAuth client configuration after installation. Each signed-in OpenNeko user, including non-admins, connects their own provider account on **Integrations**. |
| `deployment` | An administrator connects a shared deployment account through pack settings. This is the default when `scope` is omitted, preserving older manifests. |

SSO signs a user into OpenNeko; it does not connect their external provider
account. Personal connections require GraphJin **3.20.77 or later**, a JWT-enabled
organization GraphJin data source, and an authenticated OpenNeko user. Declare
`>=3.20.77 <4.0.0` for the applicable GraphJin compatibility modes.

The following is a manifest fragment for a personal connection. Replace the
example endpoints and scopes with the provider's actual contract:

```yaml
inputs:
  - key: my-pack.oauth_client_id
    type: string
    required: false
    description: OAuth client ID from your provider application.
secrets:
  - key: my-pack.oauth_client_secret
    purpose: pack_oauth_client
    required: false
  - key: my-pack.access_token
    purpose: pack_oauth_token
    required: false
  - key: my-pack.refresh_token
    purpose: pack_oauth_token
    required: false
oauth:
  - key: account
    providerLabel: Example Service
    scope: user
    experience:
      description: Connect your account to read and update your work items.
      setupInstructions: Create a web OAuth client and register the redirect URI shown below. Save the client ID and secret; each user then connects on Integrations.
      helpUrl: https://developer.example.com/oauth
    authorizationUrl: https://accounts.example.com/authorize
    tokenUrl: https://accounts.example.com/token
    userInfoUrl: https://api.example.com/me
    clientIdInput: my-pack.oauth_client_id
    clientSecret: my-pack.oauth_client_secret
    accessToken: my-pack.access_token
    refreshToken: my-pack.refresh_token
    scopes: [openid, email, work.read, work.write]
    accountIdField: sub
    accountLabelField: email
permissions:
  network: [accounts.example.com, api.example.com]
```

Keep these client and token fields optional (`required: false`) to support
installing before client setup and user consent. They are still required by the
OAuth connection flow when used. `clientIdInput` must reference a declared input;
`clientSecret`, `accessToken`, and `refreshToken` must reference declared secrets.
Use `authorizationParams` for provider-specific consent parameters, such as
requesting offline access. Account ID and label fields default to `sub` and
`email`. All OAuth endpoints must use HTTPS, and their hosts and API source hosts
must be declared in `permissions.network`.

`experience` is optional. When present, `description` is required (up to 500
characters) and appears on Integrations. `setupInstructions` (up to 4,000
characters) appears with the administrator's generated callback URI;
`helpUrl` must be HTTPS and supplies the connection help link. `providerLabel`
names the connection, and `scopes` supplies the permissions list. Do not hardcode
a deployment URL or callback handler into the pack.

In each personal API source, keep the normal bearer reference:

```yaml
auth:
  type: bearer
  token: "{{secret.my-pack.access_token}}"
```

OpenNeko recognizes the matching user-scoped OAuth declaration and generates
GraphJin's request-credential binding. Do not author internal header names or
embed a user's token in generated source configuration. OpenNeko supplies the
acting user's access token for each authorized request and refreshes it as needed.

OpenNeko owns personal token storage: encrypted credentials in its metadata
PostgreSQL database (`neko` by default), table `pack_user_connection`, scoped by
organization, user, pack installation, and connection key. The pack declares
ownership through `scope: user`; it does not choose a database or table. Shared
OAuth client credentials remain deployment configuration. Disconnecting removes
that user's credentials and invalidates account-bound approvals; removing the
pack deletes its personal connections. Changing shared client configuration
invalidates existing personal connections. When upgrading from deployment to
user scope, require each user to reconnect; shared tokens are not migrated into
personal accounts.

## Validate and exercise the pack

From the repository root, with workspace dependencies installed, validate the
small example through the same bundle loader used by installation. Create its
empty artifact directories first; Git does not preserve empty directories:

```sh
mkdir -p apps/worker/test/fixtures/service-health/{actions,policies}
pnpm --filter @neko/worker exec tsx -e 'import { loadSolutionPack } from "@neko/packs"; loadSolutionPack(process.argv[1]).then(b => console.log(b.manifest.metadata.id, b.artifacts.length)).catch(e => { console.error(e.message); process.exitCode = 1; })' ./test/fixtures/service-health
```

Replace the final path with your pack directory. Relative paths in this command
resolve from `apps/worker`; use an absolute path for a pack in another repository.

Bundle validation checks structure and references. It does not prove provider
compatibility or successful end-user execution. Before submitting a change:

1. Add focused regression coverage for changed behavior, using the existing pack
   and worker test suites.
2. Install in an isolated stack: use one-click installation for built-ins or upload
   a custom archive. Review configuration, the plan, and readiness results.
3. Run affected saved queries, metrics, workflows, and watchers against the actual
   provider. Check persisted results, not just successful HTTP responses.
4. For supported writes, check the provider state, applied receipt, approval,
   reconciliation, and any advertised undo behavior. Use dedicated test records.
5. For personal OAuth, verify admin-only client setup and two separate non-admin
   accounts. Check account isolation, refresh, reconnect, disconnect, callback
   replay rejection, and stale approval rejection after account changes. Verify
   installed-but-unconfigured and uninstalled states on Integrations.
6. Exercise upgrade behavior, including locally modified installed artifacts.

Relevant suites include `pnpm --filter @neko/packs test` and the worker's
`pack-declarative`, `pack-connector.integration`, `pack-lifecycle.integration`, and
`pack-uploads` tests. Integration tests need their documented test dependencies;
a skipped test is not a live pass.

## Save changes, submit a PR, and release

The pack's Git source is the reusable authoring source of truth. Edit its files,
validate and live-test the affected behavior, increment `metadata.version` for a
changed release, and submit a PR to the repository that owns the pack. Describe
the resulting behavior, compatibility changes, and validation evidence.

For a custom pack, publish a ZIP containing `<pack-id>/pack.yaml` and the declared
artifacts. Users upload the new version, review it, and upgrade. Changed content
cannot replace an already uploaded version. See [archive limits and release
commands](docs/CUSTOM_PACKS.md).

Magento is maintained under `packs/magento/` in this repository. Submit Magento
pack changes here; reserved first-party IDs cannot be replaced by custom uploads.
Merged first-party changes must be included in a shipped OpenNeko build before
users can upgrade to them.

### Release through a context repository

An administrator can connect a git repository in **Admin → Settings → Context
repository**. OpenNeko then reads packs from that repository's branch:

1. Put each pack at `packs/<pack-id>/`, with `pack.yaml` at the top of that folder.
   The folder name must equal `metadata.id`.
2. Omit each artifact path that has no files. Git does not keep empty folders, so
   a declared empty folder, such as `actions: actions` with no actions, fails
   validation.
3. Increment `metadata.version` for every changed release. If a pack's content
   changes under a version that OpenNeko already holds, OpenNeko reports it and
   does not add it.
4. In OpenNeko, select **Check for updates** and then **Add** for the new pack or
   version. OpenNeko validates the folder with the same rules as a ZIP upload and
   stores it as a custom pack version. Install or upgrade it on the Packs page
   with the usual review.

Reserved first-party IDs, such as `magento`, cannot be added this way.

Work chat can save individual workflow and skill changes, but does not write
those changes back into a versioned pack bundle. Installed customizations are
preserved as local changes; pack upgrades refuse conflicting edits. To share a
customization, port it into the pack's Git source and submit a PR. There is
currently no in-app pack editor or automatic export of these edits as a release.

## Declarative boundary

Packs do not install executables, container images, CLIs or dependencies. Package
skills, workflows and other supported declarative artifacts. Use GraphJin API
sources for REST access. Browser OAuth and token refresh must use supported
platform authentication; a bearer source declaration alone does not supply them.
