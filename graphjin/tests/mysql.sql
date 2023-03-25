USE db;
CREATE TABLE users (
  id BIGINT NOT NULL PRIMARY KEY,
  full_name VARCHAR(255) NOT NULL,
  phone VARCHAR(255),
  avatar VARCHAR(255),
  stripe_id VARCHAR(255),
  email VARCHAR(255) NOT NULL,
  category_counts json,
  disabled bool DEFAULT false,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP,
  UNIQUE (email)
);
CREATE TABLE categories (
  id BIGINT NOT NULL PRIMARY KEY,
  name VARCHAR(255) NOT NULL,
  description VARCHAR(255),
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP,
  CHECK (length(name) < 100),
  CHECK (length(description) < 300)
);
CREATE TABLE products (
  id BIGINT NOT NULL PRIMARY KEY,
  name VARCHAR(255),
  description VARCHAR(255),
  tags VARCHAR(255),
  metadata json,
  country_code VARCHAR(3),
  price FLOAT(7, 1),
  count_likes INTEGER,
  owner_id BIGINT,
  category_ids VARCHAR(255) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP,
  CHECK (
    length(name) > 1
    AND length(name) < 200
  ),
  CHECK (
    length(name) > 1
    AND length(name) < 50
  ),
  FOREIGN KEY (owner_id) REFERENCES users(id),
  FULLTEXT(name, description)
);
CREATE TABLE purchases (
  id BIGINT NOT NULL PRIMARY KEY,
  customer_id BIGINT,
  product_id BIGINT,
  quantity INTEGER,
  returned_at TIMESTAMP,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP,
  FOREIGN KEY (customer_id) REFERENCES users(id),
  FOREIGN KEY (product_id) REFERENCES products(id)
);
CREATE TABLE notifications (
  id BIGINT NOT NULL PRIMARY KEY,
  verb VARCHAR(255),
  subject_type VARCHAR(255),
  subject_id BIGINT,
  user_id BIGINT,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP,
  FOREIGN KEY (user_id) REFERENCES users(id)
);
CREATE TABLE comments (
  id BIGINT NOT NULL PRIMARY KEY,
  body VARCHAR(255),
  product_id BIGINT,
  commenter_id BIGINT,
  reply_to_id BIGINT,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP,
  CHECK (
    length(body) > 1
    AND length(body) < 200
  ),
  FOREIGN KEY (product_id) REFERENCES products(id),
  FOREIGN KEY (commenter_id) REFERENCES users(id),
  FOREIGN KEY (reply_to_id) REFERENCES comments(id)
);
CREATE TABLE chats (
  id BIGINT NOT NULL PRIMARY KEY,
  body VARCHAR(500),
  reply_to_id BIGINT,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP,
  FOREIGN KEY (reply_to_id) REFERENCES chats(id)
);
CREATE VIEW hot_products AS (
  SELECT id as product_id,
    country_code
  FROM products
  WHERE id > 50
);
-- CREATE TABLE chats (
--   id BIGINT NOT NULL PRIMARY KEY,
--   body VARCHAR(255),
--   reply_to_id BIGINT[],
--   created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
--   updated_at TIMESTAMP
-- );
-- sequence table since mysql does not have a row generator
CREATE TABLE seq100 (i INT NOT NULL AUTO_INCREMENT PRIMARY KEY);
INSERT INTO seq100
VALUES (),
  (),
  (),
  (),
  (),
  (),
  (),
  (),
  (),
  ();
INSERT INTO seq100
VALUES (),
  (),
  (),
  (),
  (),
  (),
  (),
  (),
  (),
  ();
INSERT INTO seq100
VALUES (),
  (),
  (),
  (),
  (),
  (),
  (),
  (),
  (),
  ();
INSERT INTO seq100
VALUES (),
  (),
  (),
  (),
  (),
  (),
  (),
  (),
  (),
  ();
INSERT INTO seq100
VALUES (),
  (),
  (),
  (),
  (),
  (),
  (),
  (),
  (),
  ();
INSERT INTO seq100
SELECT i + 50
FROM seq100;
-- insert users
INSERT INTO users (
    id,
    full_name,
    email,
    stripe_id,
    category_counts,
    disabled,
    created_at
  )
SELECT i,
  CONCAT('User ', i),
  CONCAT('user', i, '@test.com'),
  CONCAT('payment_id_', (i + 1000)),
  '[{"category_id": 1, "count": 400},{"category_id": 2, "count": 600}]',
  (
    CASE
      WHEN i = 50 THEN true
      ELSE false
    END
  ),
  '2021-01-09 16:37:01'
FROM seq100;
-- insert categories
INSERT INTO categories (id, name, description, created_at)
SELECT i,
  CONCAT('Category ', i),
  CONCAT('Description for category ', i),
  '2021-01-09 16:37:01'
FROM seq100
LIMIT 5;
-- insert products
INSERT INTO products (
    id,
    name,
    description,
    tags,
    metadata,
    country_code,
    category_ids,
    price,
    owner_id,
    created_at
  )
SELECT i,
  CONCAT('Product ', i),
  CONCAT('Description for product ', i),
  (
    SELECT GROUP_CONCAT(
        CONCAT('Tag ', i)
        ORDER BY i ASC SEPARATOR ','
      )
    FROM seq100
    WHERE i <= 5
  ),
  (
    CASE
      WHEN ((i % 2) = 0) THEN '{"foo": true}'
      ELSE '{"bar": true}'
    END
  ),
  'US',
  (
    SELECT JSON_ARRAYAGG(i)
    FROM seq100
    WHERE i <= 5
  ),
  (i + 10.5),
  i,
  '2021-01-09 16:37:01'
FROM seq100;
-- insert purchases
INSERT INTO purchases (
    id,
    customer_id,
    product_id,
    quantity,
    created_at
  )
SELECT i,
  (
    CASE
      WHEN i >= 100 THEN 1
      ELSE (i + 1)
    END
  ),
  i,
  (i * 10),
  '2021-01-09 16:37:01'
FROM seq100;
-- insert notifications
INSERT INTO notifications (
    id,
    verb,
    subject_type,
    subject_id,
    user_id,
    created_at
  )
SELECT i,
  (
    CASE
      WHEN ((i % 2) = 0) THEN 'Bought'
      ELSE 'Joined'
    END
  ),
  (
    CASE
      WHEN ((i % 2) = 0) THEN 'products'
      ELSE 'users'
    END
  ),
  i,
  (
    CASE
      WHEN i >= 2 THEN i - 1
      ELSE NULL
    END
  ),
  '2021-01-09 16:37:01'
FROM seq100;
-- insert comments
INSERT INTO comments (
    id,
    body,
    product_id,
    commenter_id,
    reply_to_id,
    created_at
  )
SELECT i,
  CONCAT('This is comment number ', i),
  i,
  i,
  (
    CASE
      WHEN i >= 2 THEN i - 1
      ELSE NULL
    END
  ),
  '2021-01-09 16:37:01'
FROM seq100;
-- insert chats
INSERT INTO chats (id, body, created_at)
SELECT i,
  CONCAT('This is chat message number ', i),
  '2021-01-09 16:37:01'
FROM seq100
LIMIT 5;
-- SET GLOBAL log_bin_trust_function_creators = 1;
-- CREATE FUNCTION is_hot_product(id bigint) 
-- RETURNS BOOL
-- READS SQL DATA
-- DETERMINISTIC
-- BEGIN
-- 	DECLARE v BOOL;
-- 	SET v = True;
--   SELECT EXISTS (SELECT p.product_id FROM hot_products p where p.product_id = id) INTO @v;
-- 	RETURN v;
-- END;
-- graph relationships
CREATE TABLE graph_node (
  id VARCHAR(10) NOT NULL PRIMARY KEY,
  label VARCHAR(10)
);
CREATE TABLE graph_edge (
  src_node VARCHAR(10),
  dst_node VARCHAR(10),
  FOREIGN KEY (src_node) REFERENCES graph_node(id),
  FOREIGN KEY (dst_node) REFERENCES graph_node(id)
);
INSERT INTO graph_node (id, label)
VALUES ('a', 'node a'),
  ('b', 'node b'),
  ('c', 'node c');
INSERT INTO graph_edge (src_node, dst_node)
VALUES ('a', 'b'),
  ('a', 'c')