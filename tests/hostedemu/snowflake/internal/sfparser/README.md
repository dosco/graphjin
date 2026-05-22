# Snowflake ANTLR Parser

This package checks in generated Go parser code so normal GraphJin tests do not
need Java or ANTLR installed.

The grammar files are Snowflake grammar files from the ANTLR grammar ecosystem,
and the generated Go code was bootstrapped from `github.com/gedhean/parser`
`v0.0.1`. Use `regenerate.sh` only when intentionally updating the grammar or
ANTLR runtime version.
