-- Foreign key metadata table: stores FK relationships for GraphJin schema discovery.
-- We avoid actual FK constraints because the DuckDB-based Snowflake emulator has a bug
-- where UPDATE on rows referenced by FK child rows fails for certain column types.
-- Real Snowflake doesn't enforce FKs anyway (they're metadata-only).
CREATE TABLE _gj_fk_metadata (
  table_schema VARCHAR,
  table_name VARCHAR,
  column_name VARCHAR,
  foreign_table_schema VARCHAR,
  foreign_table_name VARCHAR,
  foreign_column_name VARCHAR
);

INSERT INTO _gj_fk_metadata VALUES
  ('main', 'products', 'owner_id', 'main', 'users', 'id'),
  ('main', 'purchases', 'customer_id', 'main', 'users', 'id'),
  ('main', 'purchases', 'product_id', 'main', 'products', 'id'),
  ('main', 'notifications', 'user_id', 'main', 'users', 'id'),
  ('main', 'comments', 'product_id', 'main', 'products', 'id'),
  ('main', 'comments', 'commenter_id', 'main', 'users', 'id'),
  ('main', 'comments', 'reply_to_id', 'main', 'comments', 'id'),
  ('main', 'chats', 'reply_to_id', 'main', 'chats', 'id'),
  ('main', 'quotations', 'customer_id', 'main', 'users', 'id'),
  ('main', 'graph_edge', 'src_node', 'main', 'graph_node', 'id'),
  ('main', 'graph_edge', 'dst_node', 'main', 'graph_node', 'id');

CREATE TABLE users (
  id BIGINT NOT NULL PRIMARY KEY,
  full_name VARCHAR NOT NULL,
  phone VARCHAR,
  avatar VARCHAR,
  stripe_id VARCHAR,
  email VARCHAR NOT NULL UNIQUE,
  category_counts JSON,
  disabled BOOLEAN DEFAULT FALSE,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP
);

CREATE TABLE categories (
  id BIGINT NOT NULL PRIMARY KEY,
  name VARCHAR NOT NULL,
  description VARCHAR,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP
);

CREATE TABLE products (
  id BIGINT NOT NULL PRIMARY KEY,
  name VARCHAR,
  description VARCHAR,
  tags VARCHAR[],
  metadata JSON,
  country_code VARCHAR,
  price DOUBLE,
  count_likes BIGINT,
  owner_id BIGINT,
  category_ids BIGINT[],
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP
);

CREATE TABLE purchases (
  id BIGINT NOT NULL PRIMARY KEY,
  customer_id BIGINT,
  product_id BIGINT,
  quantity BIGINT,
  returned_at TIMESTAMP,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP
);

CREATE TABLE notifications (
  id BIGINT NOT NULL PRIMARY KEY,
  verb VARCHAR,
  subject_type VARCHAR,
  subject_id BIGINT,
  user_id BIGINT,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP
);

CREATE TABLE comments (
  id BIGINT NOT NULL PRIMARY KEY,
  body VARCHAR,
  product_id BIGINT,
  commenter_id BIGINT,
  reply_to_id BIGINT,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP
);

CREATE TABLE chats (
  id BIGINT NOT NULL PRIMARY KEY,
  body VARCHAR,
  reply_to_id BIGINT,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP
);

CREATE VIEW hot_products AS
SELECT id AS product_id, country_code
FROM products
WHERE id > 50;

CREATE TABLE quotations (
  id BIGINT NOT NULL PRIMARY KEY,
  validity_period JSON NOT NULL,
  customer_id BIGINT,
  amount DECIMAL(10, 2),
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE graph_node (
  id VARCHAR NOT NULL PRIMARY KEY,
  label VARCHAR
);

CREATE TABLE graph_edge (
  src_node VARCHAR,
  dst_node VARCHAR
);

WITH RECURSIVE seq(i) AS (
  SELECT 1
  UNION ALL
  SELECT i + 1 FROM seq WHERE i < 100
)
INSERT INTO users (id, full_name, email, stripe_id, category_counts, disabled, created_at)
SELECT
  i,
  'User ' || i,
  'user' || i || '@test.com',
  'payment_id_' || (i + 1000),
  '[{"category_id": 1, "count": 400}, {"category_id": 2, "count": 600}]',
  CASE WHEN i = 50 THEN TRUE ELSE FALSE END,
  '2021-01-09 16:37:01'
FROM seq;

WITH RECURSIVE seq(i) AS (
  SELECT 1
  UNION ALL
  SELECT i + 1 FROM seq WHERE i < 5
)
INSERT INTO categories (id, name, description, created_at)
SELECT
  i,
  'Category ' || i,
  'Description for category ' || i,
  '2021-01-09 16:37:01'
FROM seq;

WITH RECURSIVE seq(i) AS (
  SELECT 1
  UNION ALL
  SELECT i + 1 FROM seq WHERE i < 100
)
INSERT INTO products (
  id, name, description, tags, metadata, country_code, category_ids, price, owner_id, created_at
)
SELECT
  i,
  'Product ' || i,
  'Description for product ' || i,
  ['Tag 1', 'Tag 2', 'Tag 3', 'Tag 4', 'Tag 5'],
  CASE WHEN MOD(i, 2) = 0 THEN '{"foo": true}' ELSE '{"bar": true}' END,
  'US',
  [1, 2, 3, 4, 5],
  i + 10.5,
  i,
  '2021-01-09 16:37:01'
FROM seq;

WITH RECURSIVE seq(i) AS (
  SELECT 1
  UNION ALL
  SELECT i + 1 FROM seq WHERE i < 100
)
INSERT INTO purchases (id, customer_id, product_id, quantity, created_at)
SELECT
  i,
  CASE WHEN i >= 100 THEN 1 ELSE i + 1 END,
  i,
  i * 10,
  '2021-01-09 16:37:01'
FROM seq;

WITH RECURSIVE seq(i) AS (
  SELECT 1
  UNION ALL
  SELECT i + 1 FROM seq WHERE i < 100
)
INSERT INTO notifications (id, verb, subject_type, subject_id, user_id, created_at)
SELECT
  i,
  CASE WHEN MOD(i, 2) = 0 THEN 'Bought' ELSE 'Joined' END,
  CASE WHEN MOD(i, 2) = 0 THEN 'products' ELSE 'users' END,
  i,
  CASE WHEN i >= 2 THEN i - 1 ELSE NULL END,
  '2021-01-09 16:37:01'
FROM seq;

WITH RECURSIVE seq(i) AS (
  SELECT 1
  UNION ALL
  SELECT i + 1 FROM seq WHERE i < 100
)
INSERT INTO comments (id, body, product_id, commenter_id, reply_to_id, created_at)
SELECT
  i,
  'This is comment number ' || i,
  i,
  i,
  CASE WHEN i >= 2 THEN i - 1 ELSE NULL END,
  '2021-01-09 16:37:01'
FROM seq;

WITH RECURSIVE seq(i) AS (
  SELECT 1
  UNION ALL
  SELECT i + 1 FROM seq WHERE i < 5
)
INSERT INTO chats (id, body, created_at)
SELECT
  i,
  'This is chat message number ' || i,
  '2021-01-09 16:37:01'
FROM seq;

INSERT INTO graph_node (id, label) VALUES
  ('a', 'node a'),
  ('b', 'node b'),
  ('c', 'node c');

INSERT INTO graph_edge (src_node, dst_node) VALUES
  ('a', 'b'),
  ('a', 'c');
