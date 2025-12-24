
-- SQLite Schema

CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  full_name TEXT NOT NULL,
  phone TEXT,
  avatar TEXT,
  stripe_id TEXT,
  email TEXT NOT NULL UNIQUE,
  category_counts JSON,
  disabled BOOLEAN DEFAULT 0,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP
);

CREATE TABLE categories (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP,
  CHECK (length(name) < 100),
  CHECK (length(description) < 300)
);

CREATE TABLE products (
  id INTEGER PRIMARY KEY,
  name TEXT,
  description TEXT,
  tags TEXT,
  metadata JSON,
  country_code TEXT,
  price REAL,
  count_likes INTEGER,
  owner_id INTEGER,
  category_ids TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP,
  CHECK (length(name) > 1 AND length(name) < 200),
  FOREIGN KEY (owner_id) REFERENCES users(id)
);



CREATE TABLE purchases (
  id INTEGER PRIMARY KEY,
  customer_id INTEGER,
  product_id INTEGER,
  quantity INTEGER,
  returned_at TIMESTAMP,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP,
  FOREIGN KEY (customer_id) REFERENCES users(id),
  FOREIGN KEY (product_id) REFERENCES products(id)
);

CREATE TABLE notifications (
  id INTEGER PRIMARY KEY,
  verb TEXT,
  subject_type TEXT,
  subject_id INTEGER,
  user_id INTEGER,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP,
  FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE comments (
  id INTEGER PRIMARY KEY,
  body TEXT,
  product_id INTEGER,
  commenter_id INTEGER,
  reply_to_id INTEGER,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP,
  CHECK (length(body) > 1 AND length(body) < 200),
  FOREIGN KEY (product_id) REFERENCES products(id),
  FOREIGN KEY (commenter_id) REFERENCES users(id),
  FOREIGN KEY (reply_to_id) REFERENCES comments(id)
);

CREATE TABLE chats (
  id INTEGER PRIMARY KEY,
  body TEXT,
  reply_to_id INTEGER,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP,
  FOREIGN KEY (reply_to_id) REFERENCES chats(id)
);

CREATE TABLE quotations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  validity_period JSON NOT NULL,
  customer_id INTEGER,
  amount DECIMAL(10, 2),
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (customer_id) REFERENCES users(id)
);

CREATE TABLE graph_node (
  id TEXT NOT NULL PRIMARY KEY,
  label TEXT
);

CREATE TABLE graph_edge (
  src_node TEXT,
  dst_node TEXT,
  FOREIGN KEY (src_node) REFERENCES graph_node(id),
  FOREIGN KEY (dst_node) REFERENCES graph_node(id)
);

CREATE VIEW hot_products AS
  SELECT id AS product_id, country_code FROM products WHERE id > 50;

-- Data Generation using Recursive CTE
WITH RECURSIVE seq100(i) AS (
  SELECT 1
  UNION ALL
  SELECT i + 1 FROM seq100 WHERE i < 100
)
INSERT INTO users (id, full_name, email, stripe_id, category_counts, disabled, created_at)
SELECT i,
  'User ' || i,
  'user' || i || '@test.com',
  'payment_id_' || (i + 1000),
  '[{"category_id": 1, "count": 400},{"category_id": 2, "count": 600}]',
  (CASE WHEN i = 50 THEN 1 ELSE 0 END),
  '2021-01-09 16:37:01'
FROM seq100;

WITH RECURSIVE seq100(i) AS (
  SELECT 1
  UNION ALL
  SELECT i + 1 FROM seq100 WHERE i < 100
)
INSERT INTO categories (id, name, description, created_at)
SELECT i,
  'Category ' || i,
  'Description for category ' || i,
  '2021-01-09 16:37:01'
FROM seq100
LIMIT 5;

WITH RECURSIVE seq100(i) AS (
  SELECT 1
  UNION ALL
  SELECT i + 1 FROM seq100 WHERE i < 100
)
INSERT INTO products (id, name, description, tags, metadata, country_code, category_ids, price, owner_id, created_at)
SELECT i,
  'Product ' || i,
  'Description for product ' || i,
  'Tag ' || i,
  (CASE WHEN (i % 2) = 0 THEN '{"foo": true}' ELSE '{"bar": true}' END),
  'US',
  json_array(i),
  (i + 10.5),
  i,
  '2021-01-09 16:37:01'
FROM seq100;



WITH RECURSIVE seq100(i) AS (
  SELECT 1
  UNION ALL
  SELECT i + 1 FROM seq100 WHERE i < 100
)
INSERT INTO purchases (id, customer_id, product_id, quantity, created_at)
SELECT i,
  (CASE WHEN i >= 100 THEN 1 ELSE (i + 1) END),
  i,
  (i * 10),
  '2021-01-09 16:37:01'
FROM seq100;

WITH RECURSIVE seq100(i) AS (
  SELECT 1
  UNION ALL
  SELECT i + 1 FROM seq100 WHERE i < 100
)
INSERT INTO notifications (id, verb, subject_type, subject_id, user_id, created_at)
SELECT i,
  (CASE WHEN (i % 2) = 0 THEN 'Bought' ELSE 'Joined' END),
  (CASE WHEN (i % 2) = 0 THEN 'products' ELSE 'users' END),
  i,
  (CASE WHEN i >= 2 THEN i - 1 ELSE NULL END),
  '2021-01-09 16:37:01'
FROM seq100;

WITH RECURSIVE seq100(i) AS (
  SELECT 1
  UNION ALL
  SELECT i + 1 FROM seq100 WHERE i < 100
)
INSERT INTO comments (id, body, product_id, commenter_id, reply_to_id, created_at)
SELECT i,
  'This is comment number ' || i,
  i,
  i,
  (CASE WHEN i >= 2 THEN i - 1 ELSE NULL END),
  '2021-01-09 16:37:01'
FROM seq100;

WITH RECURSIVE seq100(i) AS (
  SELECT 1
  UNION ALL
  SELECT i + 1 FROM seq100 WHERE i < 100
)
INSERT INTO chats (id, body, created_at)
SELECT i,
  'This is chat message number ' || i,
  '2021-01-09 16:37:01'
FROM seq100
LIMIT 5;

INSERT INTO quotations (id, validity_period, customer_id, amount, created_at)
VALUES
  (1, '{"issue_date": "2024-09-15T03:03:16+0000", "expiry_date": "2024-10-15T03:03:16+0000", "status": "active"}', 1, 1000.00, '2024-09-15 03:03:16'),
  (2, '{"issue_date": "2024-09-20T03:03:16+0000", "expiry_date": "2024-10-20T03:03:16+0000", "status": "pending"}', 2, 2000.00, '2024-09-20 03:03:16'),
  (3, '{"issue_date": "2024-09-10T03:03:16+0000", "expiry_date": "2024-10-10T03:03:16+0000", "status": "expired"}', 3, 1500.00, '2024-09-10 03:03:16');

INSERT INTO graph_node (id, label) VALUES ('a', 'node a'), ('b', 'node b'), ('c', 'node c');
INSERT INTO graph_edge (src_node, dst_node) VALUES ('a', 'b'), ('a', 'c');
-- FTS5 Content Table linked to products
CREATE VIRTUAL TABLE products_fts USING fts5(name, description, content='products', content_rowid='id');

-- Triggers to keep FTS in sync with products
CREATE TRIGGER products_fts_insert AFTER INSERT ON products BEGIN
  INSERT INTO products_fts(rowid, name, description) VALUES (NEW.id, NEW.name, NEW.description);
END;

CREATE TRIGGER products_fts_update AFTER UPDATE ON products BEGIN
  INSERT INTO products_fts(products_fts, rowid, name, description) VALUES('delete', OLD.id, OLD.name, OLD.description);
  INSERT INTO products_fts(rowid, name, description) VALUES (NEW.id, NEW.name, NEW.description);
END;

CREATE TRIGGER products_fts_delete AFTER DELETE ON products BEGIN
  INSERT INTO products_fts(products_fts, rowid, name, description) VALUES('delete', OLD.id, OLD.name, OLD.description);
END;

-- Rebuild FTS index from products table (runs after all inserts above)
INSERT INTO products_fts(products_fts) VALUES('rebuild');
