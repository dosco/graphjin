#!/usr/bin/env python3
"""Whole-deployment pgBackRest coordinator for OpenNeko.

The daemon owns scheduling and the internal HTTP control surface. Database
containers perform WAL archive-push against the same encrypted repository;
this process creates paired base backups, encrypts config-volume snapshots,
and records the recovery/reconciliation boundary in a global manifest.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import http.client
import http.server
import json
import os
import pathlib
import shutil
import subprocess
import tempfile
import threading
import time
import urllib.parse


REPOSITORY = pathlib.Path("/var/lib/pgbackrest")
SETS = REPOSITORY / "openneko" / "sets"
STATUS = REPOSITORY / "openneko" / "status.json"
LATEST = REPOSITORY / "openneko" / "latest"
VERIFICATION = REPOSITORY / "openneko" / "verification.json"
STANZAS = (
    {
        "name": "openneko-metadata",
        "socket": os.environ.get("OPENNEKO_BACKUP_METADATA_SOCKET", "/sockets/neko"),
        "user": os.environ.get("NEKO_PG_USER", "neko"),
        "database": os.environ.get("NEKO_PG_DATABASE", "neko"),
    },
    {
        "name": "openneko-records",
        "socket": os.environ.get("OPENNEKO_BACKUP_RECORDS_SOCKET", "/sockets/records"),
        "user": os.environ.get("RECORDS_PG_USER", "records"),
        "database": os.environ.get("RECORDS_PG_DATABASE", "records"),
    },
)
SNAPSHOT_SOURCES = pathlib.Path("/snapshots")
SNAPSHOT_NAMES = (
    "config",
    "graphjin",
    "records-graphjin",
    "host-config",
)
STORAGE_PATHS = {
    "metadata": pathlib.Path(
        os.environ.get("OPENNEKO_METADATA_VOLUME_PATH", "/var/lib/postgresql/neko")
    ),
    "records": pathlib.Path(
        os.environ.get("OPENNEKO_RECORDS_VOLUME_PATH", "/var/lib/postgresql/records")
    ),
    "staging": pathlib.Path(
        os.environ.get("OPENNEKO_STAGING_VOLUME_PATH", "/volumes/staging")
    ),
}
MAX_SAFE_INTEGER = (1 << 53) - 1
LOCK = threading.Lock()


def utc_now() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc)


def iso(value: dt.datetime | None = None) -> str:
    return (value or utc_now()).isoformat(timespec="seconds").replace("+00:00", "Z")


def atomic_json(path: pathlib.Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    staging = path.with_name(f".{path.name}.{os.getpid()}.tmp")
    staging.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    os.chmod(staging, 0o600)
    os.replace(staging, path)


def read_json(path: pathlib.Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def storage_capacity(path: pathlib.Path) -> dict:
    value = os.statvfs(path)
    fragment_size = value.f_frsize or value.f_bsize
    total_bytes = value.f_blocks * fragment_size
    free_bytes = value.f_bavail * fragment_size
    if (
        total_bytes <= 0
        or total_bytes > MAX_SAFE_INTEGER
        or free_bytes < 0
        or free_bytes > total_bytes
        or free_bytes > MAX_SAFE_INTEGER
    ):
        raise RuntimeError(f"{path} returned an invalid capacity sample")
    return {
        "totalBytes": total_bytes,
        "freeBytes": free_bytes,
        "usedPercent": ((total_bytes - free_bytes) / total_bytes) * 100,
    }


def storage_sample() -> dict:
    return {
        "sampledAt": iso(),
        **{name: storage_capacity(path) for name, path in STORAGE_PATHS.items()},
    }


def postgres_invocation(args: list[str]) -> list[str]:
    for executable in ("gosu", "su-exec"):
        if shutil.which(executable):
            return [executable, "postgres", *args]
    raise RuntimeError("neither gosu nor su-exec is available")


def command(args: list[str], *, postgres: bool = False, timeout: int = 3600) -> str:
    invocation = postgres_invocation(args) if postgres else args
    result = subprocess.run(
        invocation,
        check=False,
        capture_output=True,
        text=True,
        timeout=timeout,
        env=os.environ.copy(),
    )
    if result.returncode != 0:
        detail = (result.stderr or result.stdout).strip()
        raise RuntimeError(f"{' '.join(args[:3])} failed: {detail[-4000:]}")
    return result.stdout


def pgbackrest(stanza: str, *args: str, timeout: int = 7200) -> str:
    return command(
        ["pgbackrest", f"--stanza={stanza}", "--log-level-console=info", *args],
        postgres=True,
        timeout=timeout,
    )


def psql(cluster: dict, query: str) -> str:
    return command(
        [
            "psql",
            "-X",
            "-h",
            cluster["socket"],
            "-p",
            str(cluster.get("port", 5432)),
            "-U",
            cluster["user"],
            "-d",
            cluster["database"],
            "-Atq",
            "-v",
            "ON_ERROR_STOP=1",
            "-c",
            query,
        ],
        postgres=True,
        timeout=30,
    ).strip()


def relation_sample(cluster: dict, relation: str, order_by: str, limit: int = 100) -> dict:
    payload = psql(
        cluster,
        f"select to_jsonb(sample)::text from {relation} sample "
        f"order by {order_by} limit {int(limit)}",
    )
    return {
        "rows_sampled": 0 if not payload else len(payload.splitlines()),
        "sha256": hashlib.sha256(payload.encode("utf-8")).hexdigest(),
    }


def database_probe(cluster: dict) -> dict:
    raw = psql(
        cluster,
        "select json_build_object("
        "'sampled_at', clock_timestamp(),"
        "'wal_lsn', pg_current_wal_lsn()::text,"
        "'database_size_bytes', pg_database_size(current_database()),"
        "'transaction_id', txid_current()"
        ")::text",
    )
    probe = json.loads(raw)
    optional = (
        ("action_requests", "public.action_request"),
        ("app_states", "public.app_state"),
        ("schema_changes", "engine.schema_change"),
        ("import_runs", "engine.import_run"),
        ("import_batches", "engine.import_batch"),
        ("record_changes", "engine.record_change_log"),
    )
    for key, relation in optional:
        exists = psql(cluster, f"select to_regclass('{relation}') is not null") == "t"
        if exists:
            probe[key] = int(psql(cluster, f"select count(*) from {relation}"))
    if psql(cluster, "select to_regclass('engine.record_change_log') is not null") == "t":
        probe["record_change_sample"] = relation_sample(
            cluster,
            "engine.record_change_log",
            "id asc",
        )
    golden_relation = "public.openneko_salesforce_golden_restore_fixture"
    if psql(cluster, f"select to_regclass('{golden_relation}') is not null") == "t":
        probe["golden_fixture"] = {
            "rows": int(psql(cluster, f"select count(*) from {golden_relation}")),
            "accepted": int(
                psql(
                    cluster,
                    f"select count(*) from {golden_relation} where disposition = 'accepted'",
                )
            ),
            "rejected": int(
                psql(
                    cluster,
                    f"select count(*) from {golden_relation} where disposition = 'rejected'",
                )
            ),
            "sample": relation_sample(
                cluster,
                golden_relation,
                "object_api_name asc, row_number asc",
                limit=500,
            ),
        }
    return probe


def latest_backup(stanza: str) -> dict:
    info = json.loads(pgbackrest(stanza, "--output=json", "info"))
    if not info or not info[0].get("backup"):
        raise RuntimeError(f"{stanza} has no completed backup")
    backup = info[0]["backup"][-1]
    archive = backup.get("archive") or {}
    timestamp = backup.get("timestamp") or {}
    return {
        "label": backup["label"],
        "type": backup.get("type"),
        "archive_start": archive.get("start"),
        "archive_stop": archive.get("stop"),
        "started_at_epoch": timestamp.get("start"),
        "completed_at_epoch": timestamp.get("stop"),
    }


def initialize_stanzas() -> None:
    deadline = time.monotonic() + int(os.environ.get("OPENNEKO_BACKUP_START_TIMEOUT_SEC", "120"))
    while True:
        try:
            for cluster in STANZAS:
                pgbackrest(cluster["name"], "stanza-create", timeout=60)
                pgbackrest(cluster["name"], "check", timeout=120)
            return
        except Exception:
            if time.monotonic() >= deadline:
                raise
            time.sleep(2)


def only_changed_while_read(stderr: str) -> bool:
    lines = [line for line in stderr.splitlines() if line.strip()]
    return bool(lines) and all(line.endswith("file changed as we read it") for line in lines)


def tar_snapshot(target: str, attempts: int = 5) -> None:
    args = [
        "tar",
        "--numeric-owner",
        "--sort=name",
        "--mtime=UTC 1970-01-01",
        "--exclude=host-config/backup-key",
        "-C",
        str(SNAPSHOT_SOURCES),
        "-cf",
        target,
        *SNAPSHOT_NAMES,
    ]
    for attempt in range(1, attempts + 1):
        result = subprocess.run(args, check=False, capture_output=True, text=True, timeout=300)
        if result.returncode == 0:
            return
        if result.returncode != 1 or not only_changed_while_read(result.stderr):
            raise RuntimeError(f"tar --numeric-owner --sort=name failed: {result.stderr.strip()[-4000:]}")
        if attempt < attempts:
            time.sleep(attempt)
    print(f"config snapshot kept after live changes: {result.stderr.strip()}", flush=True)


def snapshot_configs(directory: pathlib.Path) -> dict:
    archive = directory / "config-volumes.tar.enc"
    cipher_pass = os.environ["PGBACKREST_REPO1_CIPHER_PASS"]
    with tempfile.NamedTemporaryFile(prefix="openneko-config-", suffix=".tar") as plain:
        tar_snapshot(plain.name)
        digest = hashlib.sha256(pathlib.Path(plain.name).read_bytes()).hexdigest()
        env = os.environ.copy()
        env["OPENNEKO_BACKUP_SNAPSHOT_PASS"] = cipher_pass
        result = subprocess.run(
            [
                "openssl",
                "enc",
                "-aes-256-cbc",
                "-salt",
                "-pbkdf2",
                "-in",
                plain.name,
                "-out",
                str(archive),
                "-pass",
                "env:OPENNEKO_BACKUP_SNAPSHOT_PASS",
            ],
            check=False,
            capture_output=True,
            text=True,
            env=env,
        )
        if result.returncode != 0:
            raise RuntimeError(f"config snapshot encryption failed: {result.stderr.strip()}")
    os.chmod(archive, 0o600)
    return {
        "artifact": archive.name,
        "cipher": "aes-256-cbc-pbkdf2",
        "plaintext_sha256": digest,
        "ciphertext_sha256": hashlib.sha256(archive.read_bytes()).hexdigest(),
        "volumes": list(SNAPSHOT_NAMES),
    }


def update_status(**values: object) -> dict:
    current = read_json(STATUS) if STATUS.exists() else {"format": "openneko.backup-status.v1"}
    current.update(values)
    atomic_json(STATUS, current)
    return current


def create_backup_set(reason: str = "scheduled") -> dict:
    if not LOCK.acquire(blocking=False):
        raise RuntimeError("a backup or verification operation is already running")
    started = utc_now()
    set_id = started.strftime("%Y%m%dT%H%M%SZ")
    staging = SETS / f".{set_id}.{os.getpid()}.tmp"
    final = SETS / set_id
    try:
        update_status(state="backing_up", operation="backup", started_at=iso(started), error=None)
        initialize_stanzas()
        before = {cluster["name"]: database_probe(cluster) for cluster in STANZAS}
        staging.mkdir(parents=True, exist_ok=False)
        for cluster in STANZAS:
            pgbackrest(cluster["name"], "--type=full", "backup")
        databases = []
        for cluster in STANZAS:
            databases.append(
                {
                    "stanza": cluster["name"],
                    "database": cluster["database"],
                    "backup": latest_backup(cluster["name"]),
                    "boundary_before": before[cluster["name"]],
                    "boundary_after": database_probe(cluster),
                }
            )
        config = snapshot_configs(staging)
        completed = utc_now()
        manifest = {
            "format": "openneko.backup-set.v1",
            "set_id": set_id,
            "reason": reason,
            "created_at": iso(started),
            "completed_at": iso(completed),
            "boundary": {
                "strategy": "paired_online_backups_with_reconciliation",
                "started_at": iso(started),
                "completed_at": iso(completed),
                "note": "Independent database recovery points are bounded here; restore must run application reconciliation before traffic opens.",
            },
            "databases": databases,
            "config_snapshot": config,
        }
        atomic_json(staging / "manifest.json", manifest)
        os.replace(staging, final)
        latest_staging = LATEST.with_name(f".{LATEST.name}.{os.getpid()}.tmp")
        latest_staging.write_text(set_id + "\n", encoding="utf-8")
        os.replace(latest_staging, LATEST)
        for cluster in STANZAS:
            pgbackrest(cluster["name"], "expire", timeout=600)
        update_status(
            state="ready",
            operation=None,
            last_backup_at=iso(completed),
            last_backup_set=set_id,
            failed_at=None,
            error=None,
        )
        return manifest
    except Exception as error:
        if staging.exists():
            shutil.rmtree(staging)
        update_status(
            state="failed",
            operation="backup",
            failed_at=iso(),
            error=str(error)[:4000],
        )
        raise
    finally:
        LOCK.release()


def latest_manifest() -> tuple[pathlib.Path, dict]:
    if not LATEST.exists():
        raise RuntimeError("no completed OpenNeko backup set exists")
    set_id = LATEST.read_text(encoding="utf-8").strip()
    if not set_id or "/" in set_id or ".." in set_id:
        raise RuntimeError("latest backup pointer is invalid")
    directory = SETS / set_id
    return directory, read_json(directory / "manifest.json")


def decrypt_snapshot(directory: pathlib.Path, manifest: dict, destination: pathlib.Path) -> None:
    config = manifest["config_snapshot"]
    encrypted = directory / config["artifact"]
    plain = destination / "config-volumes.tar"
    env = os.environ.copy()
    env["OPENNEKO_BACKUP_SNAPSHOT_PASS"] = os.environ["PGBACKREST_REPO1_CIPHER_PASS"]
    result = subprocess.run(
        [
            "openssl",
            "enc",
            "-d",
            "-aes-256-cbc",
            "-pbkdf2",
            "-in",
            str(encrypted),
            "-out",
            str(plain),
            "-pass",
            "env:OPENNEKO_BACKUP_SNAPSHOT_PASS",
        ],
        check=False,
        capture_output=True,
        text=True,
        env=env,
    )
    if result.returncode != 0:
        raise RuntimeError("config snapshot cannot be decrypted with this backup key")
    actual = hashlib.sha256(plain.read_bytes()).hexdigest()
    if actual != config["plaintext_sha256"]:
        raise RuntimeError("config snapshot checksum does not match its manifest")
    command(["tar", "-xf", str(plain), "-C", str(destination)], timeout=300)


def restored_probe(data: pathlib.Path, database: str, user: str, port: int) -> dict:
    socket = data.parent / f"socket-{port}"
    socket.mkdir(mode=0o700)
    command(["chown", "postgres:postgres", str(socket)])
    process = subprocess.Popen(
        postgres_invocation(
            [
                "postgres",
                "-D",
                str(data),
                "-k",
                str(socket),
                "-p",
                str(port),
                "-c",
                "listen_addresses=",
                "-c",
                "archive_mode=off",
            ]
        ),
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        text=True,
    )
    try:
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            ready = subprocess.run(
                postgres_invocation(
                    ["pg_isready", "-h", str(socket), "-p", str(port)]
                ),
                check=False,
                capture_output=True,
            )
            if ready.returncode == 0:
                break
            if process.poll() is not None:
                detail = process.stderr.read() if process.stderr else ""
                raise RuntimeError(f"restored PostgreSQL failed to start: {detail[-4000:]}")
            time.sleep(1)
        else:
            raise RuntimeError("restored PostgreSQL did not become ready")
        cluster = {
            "socket": str(socket),
            "port": port,
            "user": user,
            "database": database,
        }
        return database_probe(cluster)
    finally:
        subprocess.run(
            postgres_invocation(
                ["pg_ctl", "-D", str(data), "-m", "fast", "stop"]
            ),
            check=False,
            capture_output=True,
            timeout=30,
        )
        if process.poll() is None:
            process.terminate()
        try:
            process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            process.kill()


def verify_latest() -> dict:
    if not LOCK.acquire(blocking=False):
        raise RuntimeError("a backup or verification operation is already running")
    started = utc_now()
    work = pathlib.Path(tempfile.mkdtemp(prefix="openneko-backup-verify-", dir="/var/tmp"))
    try:
        command(["chown", "postgres:postgres", str(work)])
        update_status(state="verifying", operation="verify", started_at=iso(started), error=None)
        directory, manifest = latest_manifest()
        decrypt_snapshot(directory, manifest, work)
        reports = []
        for index, database in enumerate(manifest["databases"]):
            stanza = database["stanza"]
            target = work / stanza
            target.mkdir(mode=0o700)
            command(["chown", "postgres:postgres", str(target)])
            pgbackrest(
                stanza,
                f"--pg1-path={target}",
                f"--set={database['backup']['label']}",
                "--type=immediate",
                "--target-action=promote",
                "restore",
            )
            cluster = next(value for value in STANZAS if value["name"] == stanza)
            probe = restored_probe(target, cluster["database"], cluster["user"], 55441 + index)
            expected = database["boundary_before"]
            for count in (
                "action_requests",
                "app_states",
                "schema_changes",
                "import_runs",
                "import_batches",
                "record_changes",
            ):
                if count in expected and probe.get(count, -1) < expected[count]:
                    raise RuntimeError(
                        f"{stanza} restored {count}={probe.get(count)} below backup boundary {expected[count]}"
                    )
            for fingerprint in ("record_change_sample", "golden_fixture"):
                if fingerprint in expected and probe.get(fingerprint) != expected[fingerprint]:
                    raise RuntimeError(
                        f"{stanza} restored {fingerprint} does not match its backup boundary"
                    )
            reports.append({"stanza": stanza, "probe": probe})
        completed = utc_now()
        result = {
            "format": "openneko.backup-verification.v1",
            "status": "succeeded",
            "backup_set": manifest["set_id"],
            "started_at": iso(started),
            "completed_at": iso(completed),
            "databases": reports,
            "config_snapshot": {"status": "decrypted_and_checksum_verified"},
        }
        atomic_json(VERIFICATION, result)
        update_status(
            state="ready",
            operation=None,
            last_verification_at=iso(completed),
            last_verification_status="succeeded",
            error=None,
        )
        return result
    except Exception as error:
        result = {
            "format": "openneko.backup-verification.v1",
            "status": "failed",
            "started_at": iso(started),
            "completed_at": iso(),
            "error": str(error)[:4000],
        }
        atomic_json(VERIFICATION, result)
        update_status(
            state="failed",
            operation="verify",
            last_verification_at=iso(),
            last_verification_status="failed",
            error=str(error)[:4000],
        )
        raise
    finally:
        resolved = work.resolve()
        if resolved.parent == pathlib.Path("/var/tmp") and resolved.name.startswith(
            "openneko-backup-verify-"
        ):
            shutil.rmtree(resolved)
        LOCK.release()


def parse_target(raw: str) -> dt.datetime:
    value = raw.strip().replace("Z", "+00:00")
    try:
        target = dt.datetime.fromisoformat(value)
    except ValueError as error:
        raise RuntimeError("restore target must be an ISO-8601 timestamp") from error
    if target.tzinfo is None:
        target = target.replace(tzinfo=dt.timezone.utc)
    return target.astimezone(dt.timezone.utc)


def postgres_timestamp(value: dt.datetime) -> str:
    """Render a recovery target in PostgreSQL's accepted timestamptz form."""
    return value.astimezone(dt.timezone.utc).strftime("%Y-%m-%d %H:%M:%S+00:00")


def manifest_for_target(target: dt.datetime) -> tuple[pathlib.Path, dict]:
    choices = []
    for path in SETS.glob("*/manifest.json"):
        manifest = read_json(path)
        completed = parse_target(manifest["completed_at"])
        if completed <= target:
            choices.append((completed, path.parent, manifest))
    if not choices:
        raise RuntimeError("no backup set exists at or before the restore target")
    _, directory, manifest = max(choices, key=lambda item: item[0])
    return directory, manifest


def clear_restore_target(path: pathlib.Path) -> None:
    resolved = path.resolve()
    if resolved.parent != pathlib.Path("/restore") or resolved.name not in {
        "neko",
        "records",
        "config",
        "graphjin",
        "records-graphjin",
        "host-config",
    }:
        raise RuntimeError(f"refusing to clear unexpected restore target {resolved}")
    resolved.mkdir(parents=True, exist_ok=True)
    for entry in resolved.iterdir():
        if entry.is_dir() and not entry.is_symlink():
            shutil.rmtree(entry)
        else:
            entry.unlink()


def rewrite_recovery_paths(destination: pathlib.Path, runtime_path: pathlib.Path) -> None:
    """Replace restore-container paths embedded by pgBackRest before handoff."""
    auto_config = destination / "postgresql.auto.conf"
    content = auto_config.read_text(encoding="utf-8")
    restore_path = str(destination)
    if restore_path not in content:
        raise RuntimeError(
            f"pgBackRest recovery config does not reference restore path {restore_path}"
        )
    auto_config.write_text(
        content.replace(restore_path, str(runtime_path)),
        encoding="utf-8",
    )
    command(["chown", "postgres:postgres", str(auto_config)])
    os.chmod(auto_config, 0o600)


def restore_to(raw_target: str) -> dict:
    if os.environ.get("OPENNEKO_BACKUP_RESTORE_MODE") != "1":
        raise RuntimeError("restore is available only in the isolated restore service")
    target = parse_target(raw_target)
    if target > utc_now():
        raise RuntimeError("restore target cannot be in the future")
    directory, manifest = manifest_for_target(target)
    work = pathlib.Path(tempfile.mkdtemp(prefix="openneko-backup-restore-", dir="/var/tmp"))
    try:
        decrypt_snapshot(directory, manifest, work)
        database_targets = {
            "openneko-metadata": (
                pathlib.Path("/restore/neko"),
                pathlib.Path("/var/lib/postgresql/neko"),
            ),
            "openneko-records": (
                pathlib.Path("/restore/records"),
                pathlib.Path("/var/lib/postgresql/records"),
            ),
        }
        for database in manifest["databases"]:
            stanza = database["stanza"]
            destination, runtime_path = database_targets[stanza]
            clear_restore_target(destination)
            command(["chown", "postgres:postgres", str(destination)])
            pgbackrest(
                stanza,
                f"--pg1-path={destination}",
                f"--set={database['backup']['label']}",
                "--type=time",
                f"--target={postgres_timestamp(target)}",
                "--target-action=promote",
                "restore",
            )
            rewrite_recovery_paths(destination, runtime_path)
        config_targets = {
            "config": pathlib.Path("/restore/config"),
            "graphjin": pathlib.Path("/restore/graphjin"),
            "records-graphjin": pathlib.Path("/restore/records-graphjin"),
            "host-config": pathlib.Path("/restore/host-config"),
        }
        # Backups from releases with the removed plugin system also carry a
        # "plugins" snapshot; it has no restore target and is skipped.
        for name, destination in config_targets.items():
            clear_restore_target(destination)
            source = work / name
            if source.exists():
                shutil.copytree(source, destination, dirs_exist_ok=True, symlinks=True)
        report = {
            "format": "openneko.restore.v1",
            "status": "restored",
            "backup_set": manifest["set_id"],
            "target": iso(target),
            "completed_at": iso(),
            "requires_reconciliation": True,
        }
        atomic_json(REPOSITORY / "openneko" / "restore-report.json", report)
        return report
    finally:
        resolved = work.resolve()
        if resolved.parent == pathlib.Path("/var/tmp") and resolved.name.startswith(
            "openneko-backup-restore-"
        ):
            shutil.rmtree(resolved)


class Handler(http.server.BaseHTTPRequestHandler):
    server_version = "OpenNekoBackup/1"

    def send_json(self, status: int, value: object) -> None:
        payload = (json.dumps(value, sort_keys=True) + "\n").encode()
        self.send_response(status)
        self.send_header("content-type", "application/json")
        self.send_header("cache-control", "no-store")
        self.send_header("content-length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, message: str, *args: object) -> None:
        print(f"[openneko-backup] {message % args}", flush=True)

    def do_GET(self) -> None:
        path = urllib.parse.urlsplit(self.path).path
        if path in {"/health", "/v1/storage"}:
            try:
                value = storage_sample()
            except Exception as error:
                self.send_json(
                    503,
                    {"error": "storage_unavailable", "message": str(error)},
                )
                return
            self.send_json(200, {"status": "ok"} if path == "/health" else value)
        elif path == "/v1/backups/status":
            value = read_json(STATUS) if STATUS.exists() else {"state": "initializing"}
            if VERIFICATION.exists():
                value["verification"] = read_json(VERIFICATION)
            self.send_json(200, value)
        else:
            self.send_json(404, {"error": "not_found"})

    def do_POST(self) -> None:
        path = urllib.parse.urlsplit(self.path).path
        try:
            if path == "/v1/backups/now":
                self.send_json(200, create_backup_set("manual"))
            elif path == "/v1/backups/verify":
                self.send_json(200, verify_latest())
            else:
                self.send_json(404, {"error": "not_found"})
        except Exception as error:
            self.send_json(503, {"error": "backup_operation_failed", "message": str(error)})


def schedule_loop() -> None:
    interval = max(3600, int(os.environ.get("OPENNEKO_BACKUP_INTERVAL_SEC", "86400")))
    while True:
        try:
            status = read_json(STATUS) if STATUS.exists() else {}
            last = status.get("last_backup_at")
            due = not last or (utc_now() - parse_target(last)).total_seconds() >= interval
            if due:
                create_backup_set("scheduled")
        except Exception as error:
            print(f"[openneko-backup] scheduled backup failed: {error}", flush=True)
        time.sleep(60)


def serve() -> None:
    SETS.mkdir(parents=True, exist_ok=True)
    threading.Thread(target=schedule_loop, name="backup-scheduler", daemon=True).start()
    port = int(os.environ.get("OPENNEKO_BACKUP_PORT", "9470"))
    server = http.server.ThreadingHTTPServer(("0.0.0.0", port), Handler)
    print(f"[openneko-backup] listening on {port}", flush=True)
    server.serve_forever()


def client(method: str, path: str) -> dict:
    connection = http.client.HTTPConnection("127.0.0.1", 9470, timeout=21600)
    connection.request(method, path, body=b"" if method == "POST" else None)
    response = connection.getresponse()
    payload = json.loads(response.read())
    if response.status >= 400:
        raise RuntimeError(payload.get("message") or payload.get("error") or "backup request failed")
    return payload


def main() -> None:
    parser = argparse.ArgumentParser(prog="openneko-backup")
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("serve")
    sub.add_parser("now")
    sub.add_parser("status")
    sub.add_parser("verify")
    restore = sub.add_parser("restore")
    restore.add_argument("--to", required=True)
    args = parser.parse_args()
    if args.command == "serve":
        serve()
    elif args.command == "now":
        print(json.dumps(client("POST", "/v1/backups/now"), indent=2, sort_keys=True))
    elif args.command == "status":
        print(json.dumps(client("GET", "/v1/backups/status"), indent=2, sort_keys=True))
    elif args.command == "verify":
        print(json.dumps(client("POST", "/v1/backups/verify"), indent=2, sort_keys=True))
    elif args.command == "restore":
        print(json.dumps(restore_to(args.to), indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
