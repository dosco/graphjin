-- PostgreSQL schema for multi-DB tests
-- Contains: users, categories, products (primary relational data)

CREATE TABLE users (
  id BIGSERIAL PRIMARY KEY,
  full_name TEXT NOT NULL,
  email TEXT UNIQUE NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE categories (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE products (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  price NUMERIC(7, 1),
  owner_id BIGINT REFERENCES users(id),
  category_id BIGINT REFERENCES categories(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Insert test data (IDs 1-3 for consistency across databases)
INSERT INTO users (id, full_name, email) VALUES
  (1, 'User 1', 'user1@test.com'),
  (2, 'User 2', 'user2@test.com'),
  (3, 'User 3', 'user3@test.com');

INSERT INTO categories (id, name) VALUES
  (1, 'Electronics'),
  (2, 'Books'),
  (3, 'Clothing');

INSERT INTO products (id, name, price, owner_id, category_id) VALUES
  (1, 'Product 1', 11.5, 1, 1),
  (2, 'Product 2', 22.5, 2, 2),
  (3, 'Product 3', 33.5, 3, 3);
