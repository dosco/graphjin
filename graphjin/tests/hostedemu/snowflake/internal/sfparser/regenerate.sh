#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

antlr4 -Dlanguage=Go -package snowflake -visitor SnowflakeLexer.g4 SnowflakeParser.g4
