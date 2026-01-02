-- GraphJin MSSQL Test Schema

CREATE TABLE users (
  id BIGINT NOT NULL PRIMARY KEY,
  full_name NVARCHAR(255) NOT NULL,
  phone NVARCHAR(255),
  avatar NVARCHAR(255),
  stripe_id NVARCHAR(255),
  email NVARCHAR(255) NOT NULL,
  category_counts NVARCHAR(MAX),
  disabled BIT DEFAULT 0,
  created_at DATETIME2 NOT NULL DEFAULT GETDATE(),
  updated_at DATETIME2,
  CONSTRAINT users_email_unique UNIQUE (email)
);
GO

CREATE TABLE categories (
  id BIGINT NOT NULL PRIMARY KEY,
  name NVARCHAR(255) NOT NULL,
  description NVARCHAR(255),
  created_at DATETIME2 NOT NULL DEFAULT GETDATE(),
  updated_at DATETIME2
);
GO

CREATE TABLE products (
  id BIGINT NOT NULL PRIMARY KEY,
  name NVARCHAR(255),
  description NVARCHAR(255),
  tags NVARCHAR(MAX),
  metadata NVARCHAR(MAX),
  country_code NVARCHAR(3),
  price DECIMAL(10, 2),
  count_likes INT,
  owner_id BIGINT,
  category_ids NVARCHAR(255) NOT NULL,
  created_at DATETIME2 NOT NULL DEFAULT GETDATE(),
  updated_at DATETIME2,
  CONSTRAINT products_owner_fk FOREIGN KEY (owner_id) REFERENCES users(id)
);
GO

CREATE TABLE purchases (
  id BIGINT NOT NULL PRIMARY KEY,
  customer_id BIGINT,
  product_id BIGINT,
  quantity INT,
  returned_at DATETIME2,
  created_at DATETIME2 NOT NULL DEFAULT GETDATE(),
  updated_at DATETIME2,
  CONSTRAINT purchases_customer_fk FOREIGN KEY (customer_id) REFERENCES users(id),
  CONSTRAINT purchases_product_fk FOREIGN KEY (product_id) REFERENCES products(id)
);
GO

CREATE TABLE notifications (
  id BIGINT NOT NULL PRIMARY KEY,
  verb NVARCHAR(255),
  subject_type NVARCHAR(255),
  subject_id BIGINT,
  user_id BIGINT,
  created_at DATETIME2 NOT NULL DEFAULT GETDATE(),
  updated_at DATETIME2,
  CONSTRAINT notifications_user_fk FOREIGN KEY (user_id) REFERENCES users(id)
);
GO

CREATE TABLE comments (
  id BIGINT NOT NULL PRIMARY KEY,
  body NVARCHAR(255),
  product_id BIGINT,
  commenter_id BIGINT,
  reply_to_id BIGINT,
  created_at DATETIME2 NOT NULL DEFAULT GETDATE(),
  updated_at DATETIME2,
  CONSTRAINT comments_product_fk FOREIGN KEY (product_id) REFERENCES products(id),
  CONSTRAINT comments_commenter_fk FOREIGN KEY (commenter_id) REFERENCES users(id),
  CONSTRAINT comments_reply_fk FOREIGN KEY (reply_to_id) REFERENCES comments(id)
);
GO

CREATE TABLE chats (
  id BIGINT NOT NULL PRIMARY KEY,
  body NVARCHAR(500),
  reply_to_id BIGINT,
  created_at DATETIME2 NOT NULL DEFAULT GETDATE(),
  updated_at DATETIME2,
  CONSTRAINT chats_reply_fk FOREIGN KEY (reply_to_id) REFERENCES chats(id)
);
GO

-- View for hot products
CREATE VIEW hot_products AS
SELECT id AS product_id, country_code
FROM products
WHERE id > 50;
GO

-- Sequence table for generating test data
CREATE TABLE seq100 (i INT IDENTITY(1,1) PRIMARY KEY);
GO

-- Populate sequence table with 100 rows
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
GO

INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
GO

INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
GO

INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
GO

INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
INSERT INTO seq100 DEFAULT VALUES;
GO

-- Fill to 100 rows (need IDENTITY_INSERT ON for explicit identity values)
SET IDENTITY_INSERT seq100 ON;
INSERT INTO seq100 (i) SELECT TOP 50 i + 50 FROM seq100;
SET IDENTITY_INSERT seq100 OFF;
GO

-- Insert users
INSERT INTO users (id, full_name, email, stripe_id, category_counts, disabled, created_at)
SELECT
  i,
  CONCAT(N'User ', i),
  CONCAT(N'user', i, N'@test.com'),
  CONCAT(N'payment_id_', (i + 1000)),
  N'[{"category_id": 1, "count": 400},{"category_id": 2, "count": 600}]',
  CASE WHEN i = 50 THEN 1 ELSE 0 END,
  '2021-01-09 16:37:01'
FROM seq100;
GO

-- Insert categories
INSERT INTO categories (id, name, description, created_at)
SELECT TOP 5
  i,
  CONCAT(N'Category ', i),
  CONCAT(N'Description for category ', i),
  '2021-01-09 16:37:01'
FROM seq100;
GO

-- Insert products
INSERT INTO products (id, name, description, tags, metadata, country_code, category_ids, price, owner_id, created_at)
SELECT
  i,
  CONCAT(N'Product ', i),
  CONCAT(N'Description for product ', i),
  N'["Tag 1", "Tag 2", "Tag 3", "Tag 4", "Tag 5"]',
  CASE WHEN (i % 2) = 0 THEN N'{"foo": true}' ELSE N'{"bar": true}' END,
  N'US',
  N'[1, 2, 3, 4, 5]',
  (i + 10.5),
  i,
  '2021-01-09 16:37:01'
FROM seq100;
GO

-- Insert purchases
INSERT INTO purchases (id, customer_id, product_id, quantity, created_at)
SELECT
  i,
  CASE WHEN i >= 100 THEN 1 ELSE (i + 1) END,
  i,
  (i * 10),
  '2021-01-09 16:37:01'
FROM seq100;
GO

-- Insert notifications
INSERT INTO notifications (id, verb, subject_type, subject_id, user_id, created_at)
SELECT
  i,
  CASE WHEN (i % 2) = 0 THEN N'Bought' ELSE N'Joined' END,
  CASE WHEN (i % 2) = 0 THEN N'products' ELSE N'users' END,
  i,
  CASE WHEN i >= 2 THEN i - 1 ELSE NULL END,
  '2021-01-09 16:37:01'
FROM seq100;
GO

-- Insert comments
INSERT INTO comments (id, body, product_id, commenter_id, reply_to_id, created_at)
SELECT
  i,
  CONCAT(N'This is comment number ', i),
  i,
  i,
  CASE WHEN i >= 2 THEN i - 1 ELSE NULL END,
  '2021-01-09 16:37:01'
FROM seq100;
GO

-- Insert chats
INSERT INTO chats (id, body, created_at)
SELECT TOP 5
  i,
  CONCAT(N'This is chat message number ', i),
  '2021-01-09 16:37:01'
FROM seq100;
GO

-- Table for testing JSON path operations
CREATE TABLE quotations (
  id BIGINT IDENTITY(1,1) PRIMARY KEY,
  validity_period NVARCHAR(MAX) NOT NULL,
  customer_id BIGINT,
  amount DECIMAL(10, 2),
  created_at DATETIME2 NOT NULL DEFAULT GETDATE(),
  CONSTRAINT quotations_customer_fk FOREIGN KEY (customer_id) REFERENCES users(id)
);
GO

-- Insert test data for quotations with nested JSON structures
INSERT INTO quotations (validity_period, customer_id, amount, created_at)
VALUES
  (N'{"issue_date": "2024-09-15T03:03:16+0000", "expiry_date": "2024-10-15T03:03:16+0000", "status": "active"}', 1, 1000.00, '2024-09-15 03:03:16'),
  (N'{"issue_date": "2024-09-20T03:03:16+0000", "expiry_date": "2024-10-20T03:03:16+0000", "status": "pending"}', 2, 2000.00, '2024-09-20 03:03:16'),
  (N'{"issue_date": "2024-09-10T03:03:16+0000", "expiry_date": "2024-10-10T03:03:16+0000", "status": "expired"}', 3, 1500.00, '2024-09-10 03:03:16');
GO

-- Graph relationships for recursive queries
CREATE TABLE graph_node (
  id NVARCHAR(10) NOT NULL PRIMARY KEY,
  label NVARCHAR(10)
);
GO

CREATE TABLE graph_edge (
  src_node NVARCHAR(10),
  dst_node NVARCHAR(10),
  CONSTRAINT graph_edge_src_fk FOREIGN KEY (src_node) REFERENCES graph_node(id),
  CONSTRAINT graph_edge_dst_fk FOREIGN KEY (dst_node) REFERENCES graph_node(id)
);
GO

INSERT INTO graph_node (id, label) VALUES (N'a', N'node a'), (N'b', N'node b'), (N'c', N'node c');
GO

INSERT INTO graph_edge (src_node, dst_node) VALUES (N'a', N'b'), (N'a', N'c');
GO
