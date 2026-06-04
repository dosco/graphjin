-- Cassandra / Amazon Keyspaces schema fixture for the GraphJin Cassandra suite.
-- Partition-key-first modeling: every governed read pins the full partition key,
-- and relationships are config-declared (CQL has no foreign keys).

CREATE KEYSPACE IF NOT EXISTS gjtest
  WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};

-- One row per user, keyed by the user id (partition key).
CREATE TABLE IF NOT EXISTS gjtest.users (
  id   text PRIMARY KEY,
  name text,
  email text
);

-- Posts clustered under their owning user. Partition key user_id makes
-- `users { posts }` a bounded single-partition N+1 fetch; id orders within it.
CREATE TABLE IF NOT EXISTS gjtest.posts (
  user_id    text,
  id         text,
  title      text,
  created_at timestamp,
  PRIMARY KEY ((user_id), id)
) WITH CLUSTERING ORDER BY (id ASC);

-- One-to-one profile per user (partition key == full primary key).
CREATE TABLE IF NOT EXISTS gjtest.profiles (
  user_id text PRIMARY KEY,
  bio     text
);

-- Counter table: counters can only be incremented (SET c = c + ?), never INSERTed.
CREATE TABLE IF NOT EXISTS gjtest.post_stats (
  post_id text PRIMARY KEY,
  views   counter
);
