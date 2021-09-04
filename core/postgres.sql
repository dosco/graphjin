CREATE TABLE users (
  id BIGSERIAL PRIMARY KEY,
  full_name TEXT NOT NULL,
  phone TEXT,
  avatar TEXT,
  stripe_id TEXT,
  email TEXT UNIQUE NOT NULL,
  category_counts JSON,
  disabled bool DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ
);

CREATE TABLE categories (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL CHECK (length(name) < 100),
  description TEXT CHECK (length(description) < 300),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ
);

CREATE TABLE products (
  id BIGSERIAL PRIMARY KEY,
  name TEXT CHECK (length(name) > 1 AND length(name) < 50),
  description TEXT CHECK (length(name) > 1 AND length(name) < 200),
  tags  TEXT[],  
  country_code VARCHAR(3),  
  price NUMERIC(7,1),
  owner_id BIGINT REFERENCES users(id),

  -- tsv column is used by full-text search
  tsv tsvector GENERATED ALWAYS AS 
    (to_tsvector('english', name) || to_tsvector('english', description)) STORED,

  category_ids BIGINT[] NOT NULL CHECK 
    (cardinality(category_ids) > 0 AND cardinality(category_ids) < 10),

  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ
);

CREATE TABLE purchases (
  id BIGSERIAL PRIMARY KEY,
  customer_id BIGINT REFERENCES users(id),
  product_id BIGINT REFERENCES products(id),
  quantity integer,
  returned_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ
);

CREATE TABLE notifications (
  id BIGSERIAL PRIMARY KEY,
  verb TEXT,
  subject_type TEXT,
  subject_id BIGINT,
  user_id BIGINT REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ
);

CREATE TABLE comments (
  id BIGSERIAL PRIMARY KEY,
  body TEXT CHECK (length(body) > 1 AND length(body) < 200),
  product_id BIGINT REFERENCES products(id),
  commenter_id BIGINT REFERENCES users(id),
  reply_to_id BIGINT REFERENCES comments(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ
);

CREATE TABLE chats (
  id BIGSERIAL PRIMARY KEY,
  body text,
  reply_to_id  BIGINT REFERENCES chats(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ
);

CREATE MATERIALIZED VIEW 
  hot_products
AS (
  SELECT 
      id as product_id,
      country_code
  FROM
      products
  WHERE
      id > 50
);

-- CREATE TABLE chats (
--   id BIGSERIAL PRIMARY KEY,
--   body TEXT,
--   reply_to_id BIGINT[],
--   created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
--   updated_at TIMESTAMPTZ
-- );

-- insert users
INSERT INTO 
  users (id, full_name, email, stripe_id, category_counts, disabled, created_at) 
SELECT 
  i, 
  'User ' || i,
  'user' || i || '@test.com',
  'payment_id_' || (i + 1000),
  '[{"category_id": 1, "count": 400},{"category_id": 2, "count": 600}]',
  (CASE WHEN i = 50 THEN true ELSE false END),
  '2021-01-09 16:37:01'
 FROM 
  GENERATE_SERIES(1, 100) i;

-- insert categories
INSERT INTO 
  categories (id, name, description, created_at) 
SELECT 
  i, 
  'Category ' || i,
  'Description for category ' || i,
  '2021-01-09 16:37:01'
FROM 
  GENERATE_SERIES(1, 5) i;

-- insert products
INSERT INTO 
  products (id, name, description, tags, country_code, category_ids, price, owner_id, created_at)
SELECT 
  i, 
  'Product ' || i, 
  'Description for product ' || i,
 	(SELECT array_agg(('Tag ' || i)) FROM GENERATE_SERIES(1, 5) i),
  'US',
  (SELECT array_agg(i) FROM GENERATE_SERIES(1, 5) i),
  (i + 10.5),
  i,
  '2021-01-09 16:37:01'
FROM 
  GENERATE_SERIES(1, 100) i;

-- insert purchases
INSERT INTO 
  purchases (id, customer_id, product_id, quantity, created_at)
SELECT 
  i, 
  (CASE WHEN i >= 100 THEN 1 ELSE (i+1) END),
  i,
  (i * 10),
  '2021-01-09 16:37:01'
FROM 
  GENERATE_SERIES(1, 100) i;

-- insert notifications
INSERT INTO 
  notifications (id, verb, subject_type, subject_id, user_id, created_at)
SELECT 
  i, 
  (CASE WHEN ((i % 2) = 0) THEN 'Bought' ELSE 'Joined' END),
  (CASE WHEN ((i % 2) = 0) THEN 'products' ELSE 'users' END),
  i,
  (CASE WHEN i >= 2 THEN i - 1 ELSE NULL END),
  '2021-01-09 16:37:01'
FROM 
  GENERATE_SERIES(1, 100) i;

-- insert comments
INSERT INTO 
  comments (id, body, product_id, commenter_id, reply_to_id, created_at)
SELECT 
  i, 
  'This is comment number ' || i,
  i,
  i,
  (CASE WHEN i >= 2 THEN i - 1 ELSE NULL END),
  '2021-01-09 16:37:01'
FROM 
  GENERATE_SERIES(1, 100) i;

-- insert chats
INSERT INTO 
  chats (id, body, created_at)
SELECT 
  i, 
  'This is chat message number ' || i,
  '2021-01-09 16:37:01'
FROM 
  GENERATE_SERIES(1, 5) i;

-- refresh view hot_products
REFRESH MATERIALIZED VIEW hot_products;
