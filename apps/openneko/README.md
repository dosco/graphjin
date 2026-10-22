# openneko

The OpenNeko operator CLI — a single Go binary that supervises the Docker
stack and installs solution packs. Same binary works on an operator's
laptop, on a self-hosted server, and inside the worker container.

## Install

One line, any platform:

```bash
curl -fsSL https://openneko.app/install.sh | sh
```

The installer detects your OS/arch and installs via Homebrew on macOS or a
checksum-verified release tarball on Linux (amd64 / arm64). Then run the guided
`openneko setup`.

Manual:

- **macOS (Homebrew):** `brew install open-neko/tap/openneko`
- **Linux:** download the tarball from
  [github.com/open-neko/openneko/releases](https://github.com/open-neko/openneko/releases),
  verify it against `checksums.txt`, and put `openneko` on your `PATH`.

You also need Docker (Docker Desktop on macOS, Docker Engine on Linux).

## Usage

### Stack supervision

```bash
openneko [--instance name] setup [--mode prod|dev|demo] [--port N] \
  [--bind-address IPv4]
openneko instances                       # list configured installations
openneko start [--mode prod|dev|demo] [--detach]
openneko upgrade [--version vX.Y.Z] [--mode auto|prod|dev|demo] [--stack-only|--cli-only] [--skip-backup]
openneko stop [--volumes]
openneko status
openneko logs [service…] [-f]
openneko migrate
openneko seed adventureworks
openneko [--instance name] graphjin import <customer-config.yml|->
openneko backup [--to <file>]
openneko backup key {export,adopt}
openneko restore <file> --confirm RESTORE [--key-file <key>]
openneko reset [--all]
```

Modes:

- `prod` — core services only (default)
- `dev` — developer defaults; from a source checkout use repo-root `pnpm dev:setup` + `pnpm dev` for demo data and hot-reloaded web/worker
- `demo` — core + AdventureWorks trial bundle

The unnamed backward-compatible installation materializes its embedded Compose
files to `.openneko/runtime/` in the current working directory. A named
installation uses
`${XDG_STATE_HOME:-$HOME/.local/state}/openneko/instances/<name>/runtime/`, so
it can be managed consistently from any working directory. Its host config and
override live under `~/.config/openneko/instances/<name>/`. The CLI appends the
matching `compose.override.yml` automatically when present.

`graphjin import` accepts a YAML fragment containing `sources`, `tables`,
and/or `relationships`. Use `-` to read it from standard input. It requires a
running production installation and applies the update through GraphJin's
preview/apply path while preserving managed authentication and keystore settings.

Named instances allow multiple production stacks on one Docker host. Setup
persists the web port and bind address; Compose resources, networks included,
use `openneko-<instance>-<mode>`. Always pass `--instance <name>` to
lifecycle, pack and secrets commands for that customer.

`openneko upgrade` resolves the latest stable release (or the exact
`--version`), updates the local CLI first, and re-executes that new binary
before it pulls service and agent images at the same tag. Keeping
the CLI and stack together matters because the CLI embeds the Compose topology
used to recreate the stack. Homebrew-owned CLIs are updated through Homebrew;
standalone CLIs use the release archive and `checksums.txt` before atomic
replacement. Before touching the stack it writes an `openneko backup` and stops
if that fails (`--skip-backup` opts out). The command then recreates the recorded stack mode, runs
migrations, prunes old OpenNeko image tags, and persists the selected tag for
later starts.

Use `--stack-only` for a deliberately independent image upgrade (including a
custom non-release image tag), or `--cli-only` to update the operator binary
without touching Docker. A source-built `0.0.0-dev` CLI cannot self-update and
must use `--stack-only` or be replaced with a released CLI. Installations from
before this behavior need one final manual CLI update; subsequent
`openneko upgrade` invocations keep both sides aligned. Older installs without
a recorded mode are inferred from existing Docker Compose projects; pass
`--mode` only when multiple OpenNeko stacks exist.

### Backups

`openneko backup` writes one encrypted file containing a `pg_dump` of neko-db
and archives of the config volumes (`openneko-config`, and
`openneko-graphjin-cli` with the customer GraphJin config and Library files;
the demo's `graphjin-config` when present). The stack must be running. Files
go to `${XDG_DATA_HOME:-$HOME/.local/share}/openneko/backups/<installation>/<project>/`
unless `--to` or `OPENNEKO_BACKUP_DIR` names another place, such as a NAS.
There is no backup service and no point-in-time restore: a backup is a file.

`openneko restore <file> --confirm RESTORE` decrypts and verifies the whole
file before changing anything, then stops the stack, replaces the config
volumes, recreates the database from the dump and starts the stack again,
whose migrations bring an older backup forward. `openneko doctor` reports the
latest backup and warns when there is none or it is more than a week old.

Each installation has its own backup key, created on first use under
`${XDG_STATE_HOME:-$HOME/.local/state}/openneko/backup-keys/` (or set
`OPENNEKO_BACKUP_KEY_FILE`). Keep a copy away from the backups:

```bash
openneko backup key export --to /secure/off-host/openneko-backup.key
```

On a new host, adopt it, or pass it to a single restore:

```bash
openneko backup key adopt --from /secure/off-host/openneko-backup.key
openneko restore backup.openneko-backup --confirm RESTORE --key-file /secure/off-host/openneko-backup.key
```

Files are AES-256-GCM in authenticated 64 KiB chunks with a per-file key
derived from the backup key, so a wrong key, truncation or any edit is
rejected.

### Skills and secrets

```bash
openneko install <git-url>[#<sub-path>]
openneko secrets {list,set,unset}
openneko doctor
```

`openneko install <https-url>[#<sub-path>]` clones the URL (shallow),
validates the agentskills.io-spec `SKILL.md` at the root or the given
sub-path, and copies the folder under `~/.openneko/skills/<skill-name>/`.
URLs must be https against github / gitlab / codeberg. Gated by the
deployment install policy (`allowGitUrlInstalls`, set at
`/admin/settings/security`).

Integrations ship as solution packs (`openneko pack …`, see
[PACKS.md](../../PACKS.md)). The plugin commands (`init`, `list`, `remove`,
`marketplace`) were removed with the plugin system.

The binary manages two things on the operator's host:

- `~/.config/openneko/secrets.json` (mode 0600) — deployment secrets by
  section (for example `channel.slack`, `signin.scalekit`), plus
  `_operators[opId][connector]` slots for per-operator OAuth credentials.
- `~/.openneko/skills/` — community skill folders the worker loads at
  agent-turn time. `pnpm skills:check` validates declared deps.

See the repo-root [README](../../README.md) for the full operator-side
docs including `/integrations` (per-operator OAuth) and `/settings/security`
(install-policy switches).

## Building from source

```bash
cd apps/openneko
go build -o openneko ./cmd/openneko
./openneko version
```

## Testing

```bash
go test ./... -count=1
# Integration tests (need docker on the host):
go test -tags=integration -count=1 -timeout 10m ./internal/db/...
```

## License

Apache License 2.0. See [LICENSE](./LICENSE), [LICENSING.md](../../LICENSING.md), and [TRADEMARKS.md](../../TRADEMARKS.md) for the full license model.
