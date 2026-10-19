# Plugins (removed)

OpenNeko no longer has a plugin system. Integrations ship as **solution packs**;
see [PACKS.md](PACKS.md).

What moved where:

- **Slack, Telegram and the HUD** are built-in channels. A pack turns one on by
  listing it under `channels:` in its manifest, and an admin adds the channel's
  credentials at **Admin → Settings → Channels**.
- **Scalekit SSO, directory sync and email-link (magic-link) sign-in** are built
  into the core. Configure them at **Admin → Settings → Sign-in** and
  **Admin → Settings → Single sign-on**.
- **Community skills** still install from a git URL with
  `openneko install <git-url>[#<sub-path>]`, once an admin enables Git-URL skill
  installs at **Admin → Settings → Security**.
- `openneko marketplace`, `openneko init`, `openneko list` and `openneko remove`,
  `openneko.plugins.json`, and the plugin-base sandbox image are gone.
