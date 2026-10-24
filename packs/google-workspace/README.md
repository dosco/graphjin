# Google Workspace solution pack

This pack uses Google Workspace REST APIs through declarative GraphJin sources.
It does not install the `gws` CLI, executables, containers, plugins, or package
dependencies.

The read surface covers Gmail, Drive, Calendar, Sheets, Docs, and Slides.
The OpenAPI specifications expose reviewed operations only. The customer owns
the Google Cloud project and OAuth client. OpenNeko does not use a plugin or run
a connector executable.

## Connect an account

1. In Google Cloud, enable the Workspace APIs listed in `pack.yaml`.
2. Configure the OAuth consent screen.
3. Create a Web application OAuth client.
4. Open Admin, Settings, Packs and click Install on Google Workspace. Its configuration then becomes available.
5. Copy the redirect URI shown under Provider setup into the OAuth client.
6. Enter the client ID and client secret, then choose Save setup.
7. Review and apply the configuration to activate the pack.
8. Each signed-in user opens Integrations, finds My connections, and connects their own Google account.

OpenNeko calculates the redirect URI from `OPENNEKO_PUBLIC_URL`. If that setting
is absent, it uses the URL that opened the Admin page. This supports custom
ports and hosted domains without a fixed callback address.

The OAuth client secret remains in the deployment secret store. Access and
refresh tokens are encrypted in separate database rows for each user and pack
installation. Tokens refresh on use without rewriting the GraphJin configuration.
Disconnecting removes only that user's credentials and invalidates their pending
account-bound approvals. Removing the pack deletes its personal credentials.

Version 0.2.0 replaces the old shared-account model. Upgrade explicitly, then
reconnect each user on Integrations; old shared access tokens are not adopted.
Personal connections require GraphJin 3.20.77 or later, which includes the
request-credential bridge for multi-user OAuth (dosco/graphjin PR #638).
OpenNeko pins GraphJin to 3.21.16. This pack needs GraphJin 3.20.77 or later.

The upstream Google Workspace CLI skill catalog is used as a task-coverage
reference. Its CLI commands are not copied into this pack. Each adopted task is
rewritten against the pack's reviewed REST operations and OpenNeko approval
rules.

## Current coverage

- Curated REST reads: Gmail messages, threads, drafts, labels, history and attachments; Drive files, permissions and account storage; Calendar lists and events; Sheets metadata and values; Docs and Slides content.
- Read skill for service-specific requests.
- Cross-application research skill for evidence gathering and handoff to other
  installed packs.
- Gmail account profile query for account-specific validation.
- Read workflows for cross-application research and Gmail plus Calendar briefings.
- Governed Gmail, Calendar, Drive, and Sheets actions. Their write policy installs disabled and an administrator must enable it before use.

## Write coverage

- Gmail: create drafts, send messages, and manage labels.
- Calendar: create, replace, and delete events.
- Drive: create, copy, move, rename, and manage sharing. Metadata operations are supported; binary upload is not.
- Sheets: update, append, clear, and batch-update values.
- Slides remains read-only.
