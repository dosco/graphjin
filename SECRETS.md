# Secrets & the vault integration

OpenNeko holds deployment secrets (channel and sign-in credentials, pack
connector keys, the model-gateway key) and
per-operator OAuth credentials. By default everything is encrypted at rest with
the local `enc:v1` cipher (AES-256-GCM, keyed from
`~/.config/openneko/secret-key`).

## External vault (Infisical)

Deployments can keep deployment-wide secret
**env bags** in a self-hosted [Infisical](https://infisical.com) instance
instead of the local file. Per-operator OAuth credentials always stay local
(`enc:v1`).

Residency split:

- Deployment env bags → Infisical, one folder per secrets section (any `/` in
  the section name is encoded as `__`, e.g. `/channel.slack/SLACK_BOT_TOKEN`).
- Per-operator OAuth tokens → local secrets file, encrypted at rest.

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
