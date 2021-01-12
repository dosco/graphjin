CREATE TABLE users (
  id BIGSERIAL PRIMARY KEY,
  full_name character varying NOT NULL,
  phone character varying,
  avatar character varying,
  stripe_id text,
  email character varying UNIQUE NOT NULL,
  category_counts json,
  disabled bool DEFAULT false,
  created_at timestamp without time zone NOT NULL DEFAULT NOW(),
  updated_at timestamp without time zone
);

CREATE TABLE categories (
  id BIGSERIAL PRIMARY KEY,
  name          text NOT NULL CHECK (length(name) < 100),
  description   text CHECK (length(description) < 300),
  created_at timestamp without time zone NOT NULL DEFAULT NOW(),
  updated_at timestamp without time zone
);

CREATE TABLE products (
  id BIGSERIAL PRIMARY KEY,
  name text CHECK (length(name) > 1 AND length(name) < 50),
  description text CHECK (length(name) > 1 AND length(name) < 200),
  tags  text[],  
  price numeric(7,2),
  owner_id bigint REFERENCES users(id),

  -- tsv column is used by full-text search
  tsv tsvector GENERATED ALWAYS AS 
    (to_tsvector('english', name) || to_tsvector('english', description)) STORED,

  category_ids bigint[] NOT NULL CHECK 
    (cardinality(category_ids) > 0 AND cardinality(category_ids) < 10),

  created_at timestamp without time zone NOT NULL DEFAULT NOW(),
  updated_at timestamp without time zone
);

CREATE TABLE purchases (
  id BIGSERIAL PRIMARY KEY,
  customer_id bigint REFERENCES users(id),
  product_id bigint REFERENCES products(id),
  quantity integer,
  returned_at timestamp without time zone,
  created_at timestamp without time zone NOT NULL DEFAULT NOW(),
  updated_at timestamp without time zone
);

CREATE TABLE notifications (
  id BIGSERIAL PRIMARY KEY,
  key character varying,
  subject_type character varying,
  subject_id bigint,
  user_id bigint REFERENCES users(id),
  created_at timestamp without time zone NOT NULL DEFAULT NOW(),
  updated_at timestamp without time zone
);

CREATE TABLE comments (
  id BIGSERIAL PRIMARY KEY,
  body text CHECK (length(body) > 1 AND length(body) < 200),
  product_id bigint REFERENCES products(id),
  commenter_id bigint REFERENCES users(id),
  reply_to_id bigint REFERENCES comments(id),
  created_at timestamp without time zone NOT NULL DEFAULT NOW(),
  updated_at timestamp without time zone
);

-- CREATE TABLE chats (
--   id BIGSERIAL PRIMARY KEY,
--   body text,
--   reply_to_id bigint[],
--   created_at timestamp without time zone NOT NULL DEFAULT NOW(),
--   updated_at timestamp without time zone
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
  '2021-01-09 16:37:01.15627+00'
 FROM 
  generate_series(1,100) i;

-- insert categories
INSERT INTO 
  categories (id, name, description, created_at) 
SELECT 
  i, 
  'Category ' || i,
  'Description for category ' || i,
  '2021-01-09 16:37:01.15627+00'
FROM 
  generate_series(1,5) i;

-- insert products
INSERT INTO 
  products (id, name, description, tags, category_ids, price, owner_id, created_at)
SELECT 
  i, 
  'Product ' || i, 
  'Description for product ' || i,
 	(SELECT array_agg(('Tag ' || i)) FROM generate_series(1, 5) i),
  (SELECT array_agg(i) FROM generate_series(1, 5) i),
  (i + 10.50),
  i,
  '2021-01-09 16:37:01.15627+00'
FROM 
  generate_series(1, 100) i;

-- insert purchases
INSERT INTO 
  purchases (id, customer_id, product_id, quantity, created_at)
SELECT 
  i, 
  (CASE WHEN i >= 100 THEN 1 ELSE (i+1) END),
  i,
  (i * 10),
  '2021-01-09 16:37:01.15627+00'
FROM 
  generate_series(1, 100) i;

-- insert comments
INSERT INTO 
  comments (id, body, product_id, commenter_id, reply_to_id, created_at)
SELECT 
  i, 
  'This is comment number ' || i,
  i,
  i,
  (CASE WHEN i >= 2 THEN i - 1 ELSE NULL END),
  '2021-01-09 16:37:01.15627+00'
FROM 
  generate_series(1, 100) i;

-- insert notifications
INSERT INTO 
  notifications (id, key, subject_type, subject_id, user_id, created_at)
SELECT 
  i, 
  (CASE WHEN ((i % 2) = 0) THEN 'Bought' ELSE 'Joined' END),
  (CASE WHEN ((i % 2) = 0) THEN 'products' ELSE 'users' END),
  i,
  (CASE WHEN i >= 2 THEN i - 1 ELSE NULL END),
  '2021-01-09 16:37:01.15627+00'
FROM 
  generate_series(1, 100) i;