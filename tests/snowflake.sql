-- Standard GraphJin test schema for real Snowflake. Matches the shape of
-- the other per-DB seed files (postgres.sql, mysql.sql, etc.) so shared
-- tests that target users/products/purchases/comments/notifications/etc.
-- work identically across backends.
--
-- Real Snowflake notes:
--   * ARRAY replaces VARCHAR[]  (Snowflake arrays are untyped).
--   * VARIANT replaces JSON     (schemaless JSON in Snowflake is VARIANT).
--   * Real PRIMARY KEY / FOREIGN KEY DDL is declared — discovery uses
--     SHOW PRIMARY KEYS / SHOW IMPORTED KEYS to surface these (see
--     core/internal/sdata/tables.go:enrichSnowflakeFromSHOW). Constraints
--     in Snowflake are informational (not enforced at write time) but
--     GraphJin needs them declared to auto-derive relationships.
--   * Seed DML uses the GENERATOR table function (Snowflake's equivalent
--     of RECURSIVE CTE for row generation) because real Snowflake does
--     not support UPDATE/INSERT via recursive CTEs the same way.
--   * PARSE_JSON(...) converts string literals to VARIANT (Snowflake
--     doesn't accept a raw JSON string where VARIANT is expected).

CREATE TABLE users (
  id BIGINT NOT NULL PRIMARY KEY,
  full_name VARCHAR NOT NULL,
  phone VARCHAR,
  avatar VARCHAR,
  stripe_id VARCHAR,
  email VARCHAR NOT NULL UNIQUE,
  category_counts VARIANT,
  disabled BOOLEAN DEFAULT FALSE,
  created_at TIMESTAMP_NTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP_NTZ
);

CREATE TABLE categories (
  id BIGINT NOT NULL PRIMARY KEY,
  name VARCHAR NOT NULL,
  description VARCHAR,
  created_at TIMESTAMP_NTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP_NTZ
);

CREATE TABLE products (
  id BIGINT NOT NULL PRIMARY KEY,
  name VARCHAR,
  description VARCHAR,
  tags ARRAY,
  metadata VARIANT,
  country_code VARCHAR,
  price DOUBLE,
  count_likes BIGINT,
  owner_id BIGINT REFERENCES users(id),
  category_ids ARRAY,
  created_at TIMESTAMP_NTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP_NTZ
);

-- Dedicated fixture for clustering/partition-key tests. Keeps the CLUSTER
-- BY behavior (auto-partition filter injection on temporal leading key)
-- isolated from the shared `products` table — otherwise the default
-- 60-day range would exclude the static 2021-dated seed rows and break
-- every non-clustering test that reads products.
CREATE TABLE events (
  id BIGINT NOT NULL PRIMARY KEY,
  event_time TIMESTAMP_NTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  region VARCHAR,
  payload VARIANT
) CLUSTER BY (event_time, region);

-- Seed with CURRENT_TIMESTAMP so the auto 60-day partition filter keeps
-- the rows visible (rows older than 60 days would be excluded).
INSERT INTO events (id, event_time, region, payload)
SELECT
  seq4() + 1,
  CURRENT_TIMESTAMP,
  CASE WHEN MOD(seq4(), 2) = 0 THEN 'US' ELSE 'EU' END,
  PARSE_JSON('{"k":"v"}')
FROM TABLE(GENERATOR(ROWCOUNT => 10));

CREATE TABLE purchases (
  id BIGINT NOT NULL PRIMARY KEY,
  customer_id BIGINT REFERENCES users(id),
  product_id BIGINT REFERENCES products(id),
  quantity BIGINT,
  returned_at TIMESTAMP_NTZ,
  created_at TIMESTAMP_NTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP_NTZ
);

CREATE TABLE notifications (
  id BIGINT NOT NULL PRIMARY KEY,
  verb VARCHAR,
  subject_type VARCHAR,
  subject_id BIGINT,
  user_id BIGINT REFERENCES users(id),
  created_at TIMESTAMP_NTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP_NTZ
);

CREATE TABLE comments (
  id BIGINT NOT NULL PRIMARY KEY,
  body VARCHAR,
  product_id BIGINT REFERENCES products(id),
  commenter_id BIGINT REFERENCES users(id),
  reply_to_id BIGINT REFERENCES comments(id),
  created_at TIMESTAMP_NTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP_NTZ
);

CREATE TABLE chats (
  id BIGINT NOT NULL PRIMARY KEY,
  body VARCHAR,
  reply_to_id BIGINT REFERENCES chats(id),
  created_at TIMESTAMP_NTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP_NTZ
);

CREATE VIEW hot_products AS
SELECT id AS product_id, country_code
FROM products
WHERE id > 50;

CREATE TABLE quotations (
  id BIGINT NOT NULL PRIMARY KEY,
  validity_period VARIANT NOT NULL,
  customer_id BIGINT REFERENCES users(id),
  amount DECIMAL(10, 2),
  created_at TIMESTAMP_NTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE graph_node (
  id VARCHAR NOT NULL PRIMARY KEY,
  label VARCHAR
);

CREATE TABLE graph_edge (
  src_node VARCHAR REFERENCES graph_node(id),
  dst_node VARCHAR REFERENCES graph_node(id)
);

INSERT INTO users (id, full_name, email, stripe_id, category_counts, disabled, created_at)
SELECT
  seq4() + 1,
  'User ' || (seq4() + 1),
  'user' || (seq4() + 1) || '@test.com',
  'payment_id_' || (seq4() + 1001),
  PARSE_JSON('[{"category_id": 1, "count": 400}, {"category_id": 2, "count": 600}]'),
  CASE WHEN (seq4() + 1) = 50 THEN TRUE ELSE FALSE END,
  '2021-01-09 16:37:01'::TIMESTAMP_NTZ
FROM TABLE(GENERATOR(ROWCOUNT => 100));

INSERT INTO categories (id, name, description, created_at)
SELECT
  seq4() + 1,
  'Category ' || (seq4() + 1),
  'Description for category ' || (seq4() + 1),
  '2021-01-09 16:37:01'::TIMESTAMP_NTZ
FROM TABLE(GENERATOR(ROWCOUNT => 5));

INSERT INTO products (id, name, description, tags, metadata, country_code, category_ids, price, owner_id, created_at)
SELECT
  seq4() + 1,
  'Product ' || (seq4() + 1),
  'Description for product ' || (seq4() + 1),
  ARRAY_CONSTRUCT('Tag 1', 'Tag 2', 'Tag 3', 'Tag 4', 'Tag 5'),
  CASE WHEN MOD(seq4() + 1, 2) = 0 THEN PARSE_JSON('{"foo": true}') ELSE PARSE_JSON('{"bar": true}') END,
  'US',
  ARRAY_CONSTRUCT(1, 2, 3, 4, 5),
  (seq4() + 1) + 10.5,
  seq4() + 1,
  '2021-01-09 16:37:01'::TIMESTAMP_NTZ
FROM TABLE(GENERATOR(ROWCOUNT => 100));

INSERT INTO purchases (id, customer_id, product_id, quantity, created_at)
SELECT
  seq4() + 1,
  CASE WHEN (seq4() + 1) >= 100 THEN 1 ELSE (seq4() + 2) END,
  seq4() + 1,
  (seq4() + 1) * 10,
  '2021-01-09 16:37:01'::TIMESTAMP_NTZ
FROM TABLE(GENERATOR(ROWCOUNT => 100));

INSERT INTO notifications (id, verb, subject_type, subject_id, user_id, created_at)
SELECT
  seq4() + 1,
  CASE WHEN MOD(seq4() + 1, 2) = 0 THEN 'Bought' ELSE 'Joined' END,
  -- Uppercase matches Snowflake's stored table names (PRODUCTS/USERS).
  -- Compiled polymorphic SQL emits `subject_type = 'PRODUCTS'` using the
  -- stored-case identifier, so lowercase seed values wouldn't match.
  CASE WHEN MOD(seq4() + 1, 2) = 0 THEN 'PRODUCTS' ELSE 'USERS' END,
  seq4() + 1,
  CASE WHEN (seq4() + 1) >= 2 THEN seq4() ELSE NULL END,
  '2021-01-09 16:37:01'::TIMESTAMP_NTZ
FROM TABLE(GENERATOR(ROWCOUNT => 100));

INSERT INTO comments (id, body, product_id, commenter_id, reply_to_id, created_at)
SELECT
  seq4() + 1,
  'This is comment number ' || (seq4() + 1),
  seq4() + 1,
  seq4() + 1,
  CASE WHEN (seq4() + 1) >= 2 THEN seq4() ELSE NULL END,
  '2021-01-09 16:37:01'::TIMESTAMP_NTZ
FROM TABLE(GENERATOR(ROWCOUNT => 100));

INSERT INTO chats (id, body, created_at)
SELECT
  seq4() + 1,
  'This is chat message number ' || (seq4() + 1),
  '2021-01-09 16:37:01'::TIMESTAMP_NTZ
FROM TABLE(GENERATOR(ROWCOUNT => 5));

INSERT INTO graph_node (id, label) VALUES
  ('a', 'node a'),
  ('b', 'node b'),
  ('c', 'node c');

INSERT INTO graph_edge (src_node, dst_node) VALUES
  ('a', 'b'),
  ('a', 'c');

CREATE TABLE product_variants (
  product_id BIGINT NOT NULL,
  variant_id BIGINT NOT NULL,
  variant_name VARCHAR NOT NULL,
  sku VARCHAR,
  PRIMARY KEY (product_id, variant_id),
  FOREIGN KEY (product_id) REFERENCES products(id)
);

CREATE TABLE order_items (
  id BIGINT NOT NULL PRIMARY KEY,
  order_id BIGINT NOT NULL,
  product_id BIGINT NOT NULL,
  variant_id BIGINT NOT NULL,
  quantity INTEGER NOT NULL DEFAULT 1,
  price DOUBLE NOT NULL,
  FOREIGN KEY (product_id, variant_id) REFERENCES product_variants(product_id, variant_id)
);

INSERT INTO product_variants (product_id, variant_id, variant_name, sku) VALUES
  (1, 1, 'Small', 'PROD1-S'),
  (1, 2, 'Medium', 'PROD1-M'),
  (1, 3, 'Large', 'PROD1-L'),
  (2, 1, 'Red', 'PROD2-R'),
  (2, 2, 'Blue', 'PROD2-B');

INSERT INTO order_items (id, order_id, product_id, variant_id, quantity, price) VALUES
  (1, 1, 1, 1, 2, 19.99),
  (2, 2, 1, 2, 1, 24.99),
  (3, 3, 1, 3, 3, 29.99),
  (4, 4, 2, 1, 1, 14.99),
  (5, 5, 2, 2, 2, 14.99);
