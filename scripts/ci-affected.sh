#!/usr/bin/env bash
#
# Decide which GraphJin CI jobs a change needs.
#
# Reads changed file paths on stdin, one per line. Paths may be relative to
# the GraphJin root or carry a leading "graphjin/" (the OpenNeko monorepo).
# Prints two lines for $GITHUB_OUTPUT:
#
#   modules=["core","serv",...]     Go modules whose unit tests must run
#   dialects=["postgres",...]       database suites that must run
#
# Usage:
#   git diff --name-only BASE HEAD | bash scripts/ci-affected.sh
#   bash scripts/ci-affected.sh --all        # every module and every dialect
#
# Rules:
#   - A module runs when it, or a GraphJin module it imports, changes.
#     wasm is left out: it builds only for js/wasm and does not build today.
#   - A dialect suite runs when its own dialect code changes, or a dialect it
#     builds on changes (mysql -> mariadb, postgres -> snowflake -> bigquery
#     and redshift, mongodb -> cassandra and clickhouse).
#   - Shared query compiler code (qcode, psql, sdata, shared dialect and
#     introspection code, the shared test harness) runs every dialect.
#   - Other engine code that the database suites import runs a smoke set.
#   - A go.mod or go.sum change runs the smoke set.
#   - Workflow, workspace, and this script run everything.

set -euo pipefail

ALL_MODULES="core agent auth conf plugin-otel codesql mongodriver cassandradriver clickhousedriver hostedemu examples serv cmd"
ALL_DIALECTS="postgres mysql mariadb sqlite oracle mssql mongodb cassandra clickhouse snowflake bigquery redshift"
SMOKE_DIALECTS="postgres sqlite"

modules=""
dialects=""

add_module() { for m in "$@"; do modules="$modules $m"; done; }
add_dialect() { for d in "$@"; do dialects="$dialects $d"; done; }

# Modules that import a given module, directly or through another module.
module_and_dependents() {
	case "$1" in
	core) echo "core agent auth conf plugin-otel serv cmd" ;;
	agent | auth | plugin-otel) echo "$1 serv cmd" ;;
	conf) echo "conf" ;;
	codesql | mongodriver | cassandradriver | clickhousedriver) echo "$1 serv cmd" ;;
	hostedemu | examples) echo "$1 cmd" ;;
	serv) echo "serv cmd" ;;
	*) echo "$1" ;;
	esac
}

# A dialect and the dialects that embed it.
dialect_and_dependents() {
	case "$1" in
	postgres) echo "postgres snowflake bigquery redshift" ;;
	snowflake) echo "snowflake bigquery redshift" ;;
	mysql) echo "mysql mariadb" ;;
	mongodb) echo "mongodb cassandra clickhouse" ;;
	*) echo "$1" ;;
	esac
}

# The dialect a path belongs to, if any.
dialect_of() {
	local p="$1"
	case "$p" in
	mongodriver/* | */mongodb.go | */mongodb_* | */mongo_* | scripts/test-mongo.sh) echo mongodb ;;
	cassandradriver/* | *cassandra*) echo cassandra ;;
	clickhousedriver/* | *clickhouse*) echo clickhouse ;;
	hostedemu/snowflake/* | tests/snowflakeemu/* | *snowflake*) echo snowflake ;;
	hostedemu/bigquery/* | tests/hostedemu/bigquery/* | *bigquery*) echo bigquery ;;
	tests/hostedemu/redshift/* | tests/redshiftemu/* | *redshift*) echo redshift ;;
	*mariadb*) echo mariadb ;;
	*mysql*) echo mysql ;;
	*sqlite* | Dockerfile.sqlite-test) echo sqlite ;;
	*mssql*) echo mssql ;;
	core/internal/dialect/oracle.go | core/internal/introspection/sql/oracle_* | \
		core/internal/psql/oracle_* | tests/oracle.sql | scripts/test-oracle.sh) echo oracle ;;
	*postgres* | tests/query_pg_test.go | tests/cockroach.sql) echo postgres ;;
	esac
}

classify() {
	local p="$1"
	p="${p#graphjin/}"

	case "$p" in
	# Docs, the website, and wasm need no Go tests here.
	*.md | docs/* | website/* | notes/* | benchmark/* | training/* | wasm/*) return ;;
	# CI plumbing and the workspace: run everything.
	go.work | go.work.sum | scripts/ci-affected.sh | scripts/test-parallel.sh | Makefile | \
		.github/workflows/graphjin.yml | .github/workflows/build.yml)
		add_module $ALL_MODULES
		add_dialect $ALL_DIALECTS
		return
		;;
	esac

	# Unit tests by module.
	case "$p" in
	core/*) add_module $(module_and_dependents core) ;;
	agent/*) add_module $(module_and_dependents agent) ;;
	auth/*) add_module $(module_and_dependents auth) ;;
	conf/*) add_module $(module_and_dependents conf) ;;
	plugin/otel/*) add_module $(module_and_dependents plugin-otel) ;;
	codesql/*) add_module $(module_and_dependents codesql) ;;
	mongodriver/*) add_module $(module_and_dependents mongodriver) ;;
	cassandradriver/*) add_module $(module_and_dependents cassandradriver) ;;
	clickhousedriver/*) add_module $(module_and_dependents clickhousedriver) ;;
	hostedemu/*) add_module $(module_and_dependents hostedemu) ;;
	examples/*) add_module $(module_and_dependents examples) ;;
	serv/*) add_module $(module_and_dependents serv) ;;
	cmd/*) add_module cmd ;;
	esac

	# Database suites. A dependency bump runs the smoke set; the nightly
	# run covers the rest.
	case "$p" in
	*/go.mod | */go.sum)
		add_dialect $SMOKE_DIALECTS
		return
		;;
	esac

	local d
	d=$(dialect_of "$p")
	if [ -n "$d" ]; then
		add_dialect $(dialect_and_dependents "$d")
		return
	fi

	case "$p" in
	# Shared query compiler, schema, and introspection code: every dialect.
	core/internal/qcode/* | core/internal/psql/* | core/internal/sdata/* | \
		core/internal/dialect/* | core/internal/introspection/* | \
		tests/*)
		add_dialect $ALL_DIALECTS
		;;
	# Other code the database suites import: a smoke set.
	core/* | serv/* | auth/* | conf/* | plugin/otel/*)
		add_dialect $SMOKE_DIALECTS
		;;
	esac
}

to_json() {
	local out="" seen=" " x
	for x in $1; do
		case "$seen" in *" $x "*) continue ;; esac
		seen="$seen$x "
		out="$out${out:+,}\"$x\""
	done
	echo "[$out]"
}

# Keep the order of the ALL_* lists so job names stay stable.
ordered() {
	local all="$1" picked="$2" out="" x
	for x in $all; do
		case " $picked " in *" $x "*) out="$out $x" ;; esac
	done
	echo "$out"
}

if [ "${1:-}" = "--all" ]; then
	modules="$ALL_MODULES"
	dialects="$ALL_DIALECTS"
else
	while IFS= read -r path || [ -n "$path" ]; do
		[ -n "$path" ] && classify "$path"
	done
fi

echo "modules=$(to_json "$(ordered "$ALL_MODULES" "$modules")")"
echo "dialects=$(to_json "$(ordered "$ALL_DIALECTS" "$dialects")")"
