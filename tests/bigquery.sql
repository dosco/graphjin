CREATE TABLE users (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  full_name STRING NOT NULL,
  phone STRING,
  avatar STRING,
  stripe_id STRING,
  email STRING NOT NULL,
  category_counts JSON,
  disabled BOOL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP
);

CREATE TABLE categories (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  name STRING NOT NULL,
  description STRING,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP
);

CREATE TABLE products (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  name STRING,
  description STRING,
  tags ARRAY<STRING>,
  metadata JSON,
  country_code STRING,
  price NUMERIC,
  count_likes INT64,
  owner_id INT64 REFERENCES users(id) NOT ENFORCED,
  category_ids ARRAY<INT64>,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP
);

CREATE TABLE events (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  event_time TIMESTAMP NOT NULL,
  region STRING,
  payload JSON
)
CLUSTER BY event_time, region;

INSERT INTO events (id, event_time, region, payload)
SELECT
  i,
  CURRENT_TIMESTAMP,
  CASE WHEN MOD(i, 2) = 0 THEN 'US' ELSE 'EU' END,
  PARSE_JSON('{"k":"v"}')
FROM UNNEST(GENERATE_ARRAY(1, 10)) AS i;

CREATE TABLE purchases (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  customer_id INT64 REFERENCES users(id) NOT ENFORCED,
  product_id INT64 REFERENCES products(id) NOT ENFORCED,
  quantity INT64,
  returned_at TIMESTAMP,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP
);

CREATE TABLE notifications (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  verb STRING,
  subject_type STRING,
  subject_id INT64,
  user_id INT64 REFERENCES users(id) NOT ENFORCED,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP
);

CREATE TABLE comments (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  body STRING,
  product_id INT64 REFERENCES products(id) NOT ENFORCED,
  commenter_id INT64 REFERENCES users(id) NOT ENFORCED,
  reply_to_id INT64 REFERENCES comments(id) NOT ENFORCED,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP
);

CREATE TABLE chats (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  body STRING,
  reply_to_id INT64 REFERENCES chats(id) NOT ENFORCED,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP
);

CREATE VIEW hot_products AS
SELECT id AS product_id, country_code
FROM products
WHERE id > 50;

CREATE TABLE quotations (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  validity_period JSON NOT NULL,
  customer_id INT64 REFERENCES users(id) NOT ENFORCED,
  amount NUMERIC,
  created_at TIMESTAMP NOT NULL
);

CREATE TABLE graph_node (
  id STRING NOT NULL PRIMARY KEY NOT ENFORCED,
  label STRING
);

CREATE TABLE graph_edge (
  src_node STRING REFERENCES graph_node(id) NOT ENFORCED,
  dst_node STRING REFERENCES graph_node(id) NOT ENFORCED
);

INSERT INTO users (id, full_name, email, stripe_id, category_counts, disabled, created_at)
SELECT
  i,
  CONCAT('User ', CAST(i AS STRING)),
  CONCAT('user', CAST(i AS STRING), '@test.com'),
  CONCAT('payment_id_', CAST(i + 1000 AS STRING)),
  PARSE_JSON('[{"category_id": 1, "count": 400}, {"category_id": 2, "count": 600}]'),
  CASE WHEN i = 50 THEN TRUE ELSE FALSE END,
  TIMESTAMP '2021-01-09 16:37:01'
FROM UNNEST(GENERATE_ARRAY(1, 100)) AS i;

INSERT INTO categories (id, name, description, created_at)
SELECT
  i,
  CONCAT('Category ', CAST(i AS STRING)),
  CONCAT('Description for category ', CAST(i AS STRING)),
  TIMESTAMP '2021-01-09 16:37:01'
FROM UNNEST(GENERATE_ARRAY(1, 5)) AS i;

INSERT INTO products (id, name, description, tags, metadata, country_code, category_ids, price, owner_id, created_at)
SELECT
  i,
  CONCAT('Product ', CAST(i AS STRING)),
  CONCAT('Description for product ', CAST(i AS STRING)),
  TO_JSON(['Tag 1', 'Tag 2', 'Tag 3', 'Tag 4', 'Tag 5']),
  CASE WHEN MOD(i, 2) = 0 THEN PARSE_JSON('{"foo": true}') ELSE PARSE_JSON('{"bar": true}') END,
  'US',
  TO_JSON([1, 2, 3, 4, 5]),
  CAST(i + 10.5 AS NUMERIC),
  i,
  TIMESTAMP '2021-01-09 16:37:01'
FROM UNNEST(GENERATE_ARRAY(1, 100)) AS i;

INSERT INTO purchases (id, customer_id, product_id, quantity, created_at)
SELECT
  i,
  CASE WHEN i >= 100 THEN 1 ELSE i + 1 END,
  i,
  i * 10,
  TIMESTAMP '2021-01-09 16:37:01'
FROM UNNEST(GENERATE_ARRAY(1, 100)) AS i;

INSERT INTO notifications (id, verb, subject_type, subject_id, user_id, created_at)
SELECT
  i,
  CASE WHEN MOD(i, 2) = 0 THEN 'Bought' ELSE 'Joined' END,
  CASE WHEN MOD(i, 2) = 0 THEN 'products' ELSE 'users' END,
  i,
  CASE WHEN i >= 2 THEN i - 1 ELSE NULL END,
  TIMESTAMP '2021-01-09 16:37:01'
FROM UNNEST(GENERATE_ARRAY(1, 100)) AS i;

INSERT INTO comments (id, body, product_id, commenter_id, reply_to_id, created_at)
SELECT
  i,
  CONCAT('This is comment number ', CAST(i AS STRING)),
  i,
  i,
  CASE WHEN i >= 2 THEN i - 1 ELSE NULL END,
  TIMESTAMP '2021-01-09 16:37:01'
FROM UNNEST(GENERATE_ARRAY(1, 100)) AS i;

INSERT INTO chats (id, body, created_at)
SELECT
  i,
  CONCAT('This is chat message number ', CAST(i AS STRING)),
  TIMESTAMP '2021-01-09 16:37:01'
FROM UNNEST(GENERATE_ARRAY(1, 5)) AS i;

INSERT INTO graph_node (id, label) VALUES
  ('a', 'node a'),
  ('b', 'node b'),
  ('c', 'node c');

INSERT INTO graph_edge (src_node, dst_node) VALUES
  ('a', 'b'),
  ('a', 'c');

CREATE TABLE product_variants (
  product_id INT64 NOT NULL,
  variant_id INT64 NOT NULL,
  variant_name STRING NOT NULL,
  sku STRING,
  PRIMARY KEY (product_id, variant_id) NOT ENFORCED,
  FOREIGN KEY (product_id) REFERENCES products(id) NOT ENFORCED
);

CREATE TABLE order_items (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  order_id INT64 NOT NULL,
  product_id INT64 NOT NULL,
  variant_id INT64 NOT NULL,
  quantity INT64 NOT NULL,
  price NUMERIC NOT NULL,
  FOREIGN KEY (product_id, variant_id) REFERENCES product_variants(product_id, variant_id) NOT ENFORCED
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
