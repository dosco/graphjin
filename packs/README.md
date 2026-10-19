# Solution packs

## First-party packs

| Pack | What it does |
| --- | --- |
| [magento](magento/README.md) | Commerce analytics and governed store changes |
| [google-workspace](google-workspace/README.md) | Gmail, Drive, Calendar, Sheets, Docs, and Slides with per-user OAuth |
| [resend](resend/README.md) | Operational email from a fixed sender |
| [slack](slack/README.md) | Slack messages, reactions, and user and channel lookups |
| [shopify](shopify/README.md) | Shopify orders, and order notes and tags |
| [parallel-search](parallel-search/README.md) | Web search through Parallel |

Resend, Slack, Shopify, and Parallel replace the plugins of the same names.
They call each vendor's REST API through GraphJin, so no third-party code runs
inside OpenNeko.

## Installation and configuration

Built-in packs install in one click from Admin, Settings, Packs. Installation registers the included definition without contacting external services or requiring credentials. The installed pack then exposes fields from `inputs`, `secrets`, and `oauth`. Reviewing and applying those settings activates its artifacts through the existing lifecycle. Until then, readiness reports that configuration is required. Uploaded packs retain the archive review and installation flow.

An optional `management: { label, path }` declaration links to an existing local operational screen once configuration is applied. Ordinary configuration and OAuth setup always use the shared renderer.

## Release from a context repository

If an administrator connects a [context repository](../CONTEXT_REPOSITORY.md), OpenNeko can take packs from it. Put each pack at `packs/<pack-id>/` in the repository, with `pack.yaml` at the top of that folder. Omit artifact paths that have no files, because git does not keep empty folders. Raise `metadata.version` for every changed release. In OpenNeko, **Check for updates** lists new packs and versions, and **Add** stores them as uploaded packs for the usual review and installation. See [PACKS.md](../PACKS.md#release-through-a-context-repository).

## Pack-owned connection screens

Declare OAuth connections in `pack.yaml`. The generic Admin and Integrations
screens render the metadata; adding a provider requires no OpenNeko UI code.

```yaml
oauth:
  - key: account
    providerLabel: Example service
    scope: user # deployment retains the existing shared-account flow
    experience:
      description: Connect your account for your work and automations.
      setupInstructions: Create an OAuth web client and register the displayed callback URI.
      helpUrl: https://example.com/oauth-help
    # Also declare authorizationUrl, tokenUrl, userInfoUrl, clientIdInput,
    # clientSecret, accessToken, refreshToken, and the reviewed scopes.
```

`experience` is optional plain text, with an HTTPS help link. OpenNeko owns the
standard controls, accessibility, session binding, PKCE, encryption, and token
refresh. Packs supply content and reviewed provider endpoints, not executable UI
or scripts. `accountIdField` and `accountLabelField` select provider user-info
fields (defaults: `sub` and `email`).

For `scope: user`, source bearer tokens still reference the declared
`{{secret.<accessToken>}}` key. Installation materializes a dedicated request
header instead of a static token; users never paste tokens into pack settings.
User-scoped connections use the OAuth client configured by an admin. Personal
workflows execute as their active owner, and action approvals bind to the
requester's connection revision. Ownerless runs have no personal credentials.
