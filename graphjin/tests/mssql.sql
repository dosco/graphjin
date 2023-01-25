USE db;

CREATE TABLE users (
  id bigint PRIMARY KEY,
  full_name varchar(255) NOT NULL,
  phone varchar(255),
  avatar varchar(255),
  stripe_id varchar(255),
  email varchar(255) NOT NULL,
  category_counts nvarchar(1024),
  disabled bit DEFAULT 'FALSE',
  created_at timestamp NOT NULL DEFAULT current_timestamp
);

-- CREATE UNIQUE INDEX users_unique_email_idx ON users(email);

/*
CREATE TABLE categories (
  id bigint NOT NULL,
  name varchar(255) NOT NULL,
  description varchar(255),
  created_at timestamp NOT NULL DEFAULT current_timestamp,
  PRIMARY KEY (id),
);

CREATE TABLE products (
  id bigint NOT NULL,
  name varchar(255),
  description varchar(255),
  tags varchar(255),
  country_code varchar(3),
  price float,
  owner_id bigint,
  category_ids varchar(255) NOT NULL,
  created_at timestamp NOT NULL DEFAULT current_timestamp,
  PRIMARY KEY (id),
  FOREIGN KEY (owner_id)
  REFERENCES users (id)
);

CREATE INDEX products_name_description_idx ON products(name, description);

CREATE TABLE purchases (
  id bigint NOT NULL,
  customer_id bigint,
  product_id bigint,
  quantity int,
  returned_at timestamp,
  created_at timestamp NOT NULL DEFAULT current_timestamp,
  PRIMARY KEY (id),
  FOREIGN KEY (customer_id)
  REFERENCES users (id),
  FOREIGN KEY (product_id)
  REFERENCES products (id)
);

CREATE TABLE notifications (
  id bigint NOT NULL,
  verb varchar(255),
  subject_type varchar(255),
  subject_id bigint,
  user_id bigint,
  created_at timestamp NOT NULL DEFAULT current_timestamp,
  PRIMARY KEY (id),
  FOREIGN KEY (user_id)
  REFERENCES users (id)
);

CREATE TABLE comments (
  id bigint NOT NULL,
  body varchar(255),
  product_id bigint,
  commenter_id bigint,
  reply_to_id bigint,
  created_at timestamp NOT NULL DEFAULT current_timestamp,
  PRIMARY KEY (id),
  FOREIGN KEY (product_id)
  REFERENCES products (id),
  FOREIGN KEY (commenter_id)
  REFERENCES users (id),
  FOREIGN KEY (reply_to_id)
  REFERENCES comments (id)
);

CREATE TABLE chats (
  id bigint NOT NULL,
  body varchar(500),
  reply_to_id bigint,
  created_at timestamp NOT NULL DEFAULT current_timestamp,
  PRIMARY KEY (id),
  FOREIGN KEY (reply_to_id)
  REFERENCES chats (id)
);

CREATE VIEW hot_products
AS
SELECT id AS product_id, country_code
FROM products
WHERE id > 50;

CREATE SEQUENCE seq100 START WITH 1 INCREMENT BY 1 MAXVALUE 100;

INSERT INTO users (id, full_name, email, stripe_id, category_counts, disabled, created_at)
SELECT
  i,
  concat(
    'User ',
    CAST(i AS char)
  ),
  concat(
    'user',
    CAST(i AS char),
    '@test.com'
  ),
  concat(
    'payment_id_',
    CAST((i + 1000) AS char)
  ),
  '[{"category_id": 1, "count": 400},{"category_id": 2, "count": 600}]',
  CASE
    WHEN i = 50 THEN 'TRUE'
    ELSE 'FALSE'
  END,
  '2021-01-09 16:37:01'
FROM seq100;

INSERT INTO categories (id, name, description, created_at)
SELECT
  i,
  concat(
    'Category ',
    CAST(i AS char)
  ),
  concat(
    'Description for category ',
    CAST(i AS char)
  ),
  '2021-01-09 16:37:01'
FROM seq100
LIMIT 5;

INSERT INTO products (id, name, description, tags, country_code, category_ids, price, owner_id, created_at)
SELECT
  i,
  concat(
    'Product ',
    CAST(i AS char)
  ),
  concat(
    'Description for product ',
    CAST(i AS char)
  ),
  (
    SELECT group_concat(concat(
      'Tag ',
      CAST(i AS char)
    ) ORDER BY i ASC SEPARATOR ',')
    FROM seq100
    WHERE i <= 5
  ),
  'US',
  (
    SELECT json_merge_preserve('[]', concat(
      '[',
      group_concat(i SEPARATOR ','),
      ']'
    ))
    FROM seq100
    WHERE i <= 5
  ),
  (i + 10.5),
  i,
  '2021-01-09 16:37:01'
FROM seq100;

INSERT INTO purchases (id, customer_id, product_id, quantity, created_at)
SELECT
  i,
  CASE
    WHEN i >= 100 THEN 1
    ELSE (i + 1)
  END,
  i,
  (i * 10),
  '2021-01-09 16:37:01'
FROM seq100;

INSERT INTO notifications (id, verb, subject_type, subject_id, user_id, created_at)
SELECT
  i,
  CASE
    WHEN MOD(i, 2) = 0 THEN 'Bought'
    ELSE 'Joined'
  END,
  CASE
    WHEN MOD(i, 2) = 0 THEN 'products'
    ELSE 'users'
  END,
  i,
  CASE
    WHEN i >= 2 THEN (i - 1)
    ELSE NULL
  END,
  '2021-01-09 16:37:01'
FROM seq100;

INSERT INTO comments (id, body, product_id, commenter_id, reply_to_id, created_at)
SELECT
  i,
  concat(
    'This is comment number ',
    CAST(i AS char)
  ),
  i,
  i,
  CASE
    WHEN i >= 2 THEN (i - 1)
    ELSE NULL
  END,
  '2021-01-09 16:37:01'
FROM seq100;

INSERT INTO chats (id, body, created_at)
SELECT
  i,
  concat(
    'This is chat message number ',
    CAST(i AS char)
  ),
  '2021-01-09 16:37:01'
FROM seq100
LIMIT 5;
*/