# Secrets & the vault integration

OpenNeko holds deployment secrets (channel and sign-in credentials, pack
connector keys) and per-user pack OAuth credentials. By default everything is encrypted at rest with
the local `enc:v1` cipher (AES-256-GCM, keyed from
`~/.config/openneko/secret-key`).

## One key root

GraphJin's keystore (`gjsecret://` references in its config, sealed in
`secrets.enc.yml`) uses a key that OpenNeko derives from the same
`secret-key`, as it already does for GraphJin's JWT secrets. One backup of
`secret-key` and one rotation cover both.

`graphjin-secret-init` sets the derived key before GraphJin starts. On an
install that used a random keystore key, that key moves to
`secrets.keystore.previous_keys`, and GraphJin re-seals every value under the
derived key at its next start. Keep the old key with any backup taken before
this change.

## External vault (Infisical)

Deployments can keep deployment-wide secret
**env bags** in a self-hosted [Infisical](https://infisical.com) instance
instead of the local file. Per-user pack OAuth credentials always stay in
OpenNeko's database (`enc:v1`).

Residency split:

- Deployment env bags → Infisical, one folder per secrets section (any `/` in
  the section name is encoded as `__`, e.g. `/channel.slack/SLACK_BOT_TOKEN`).
- Per-user pack OAuth tokens → OpenNeko's database (table
  `pack_user_connection`), encrypted at rest.

### Configure

Set the backend in `~/.config/openneko/config.json`:

```json
{
  "secrets": {
    "backend": "infisical",
    "infisical": {
      "siteUrl": "https://infisical.internal",
      "projectId": "<project-id>",
      "environment": "prod"
    }
  }
}
```

Provide the Universal Auth machine identity via env (or the `infisical.clientId`
/ `clientSecret` fields):

```
INFISICAL_UNIVERSAL_AUTH_CLIENT_ID=...
INFISICAL_UNIVERSAL_AUTH_CLIENT_SECRET=...
```

Secrets are re-fetched on a short TTL, so a rotation in Infisical surfaces on the
next refresh without a restart.

### Fallbacks

- Infisical unreachable at runtime: OpenNeko falls back to the local file.

OpenNeko talks to Infisical's MIT-licensed REST API and bundles no Infisical
code. "Infisical" is a trademark of its owner; this integration is not an
endorsement.
