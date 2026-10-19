#!/bin/sh
# Metadata GraphJin supervisor. It decrypts the setup-managed Postgres
# password, writes the generated config atomically, and refreshes it whenever
# the shared local config changes. GraphJin's reload_on_config_change then
# reconnects without an operator-only container restart.
set -eu

config_json=/openneko-config/config.json
secret_key=/openneko-config/secret-key
seed=/seed/neko.yml
generated=/config/dev.yml

template_config() {
  openneko graphjin-config "$config_json" "$secret_key" "$seed" "$generated"
}

source_generation() {
  cksum "$config_json" "$secret_key" "$seed" 2>/dev/null || cksum "$seed"
}

mkdir -p /config
template_config

watch_config() {
  generation=$(source_generation)
  while :; do
    next_generation=$(source_generation)
    if [ "$generation" != "$next_generation" ]; then
      if template_config; then
        generation=$next_generation
      else
        echo "[neko-graphjin] refusing invalid credential refresh; retaining the last valid config" >&2
      fi
    fi
    sleep 1
  done
}

watch_config &
watch_pid=$!

shutdown() {
  kill -TERM "$watch_pid" 2>/dev/null || true
  if [ -n "${graphjin_pid:-}" ]; then
    kill -TERM "$graphjin_pid" 2>/dev/null || true
  fi
}
trap shutdown INT TERM

graphjin "$@" &
graphjin_pid=$!
set +e
wait "$graphjin_pid"
status=$?
set -e
kill -TERM "$watch_pid" 2>/dev/null || true
wait "$watch_pid" 2>/dev/null || true
exit "$status"
