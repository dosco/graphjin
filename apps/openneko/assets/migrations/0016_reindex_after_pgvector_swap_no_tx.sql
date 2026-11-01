-- Historical compatibility marker. This migration originally reindexed the
-- hard-coded `neko` database after an Alpine/musl -> Debian/glibc image swap.
-- That was not durable: it did not cover custom database names, records-db,
-- existing installations that had already recorded this file, or a later
-- storage-runtime change in the opposite direction.
--
-- Storage reconciliation now runs before schema migrations, uses each
-- connection's actual database identifier, repairs both managed databases,
-- refreshes the recorded collation version, verifies the result, and records
-- the enforced storage contract independently of schema_migrations.

SELECT 1;
