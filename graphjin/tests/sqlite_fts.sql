-- Test FTS Table
CREATE VIRTUAL TABLE products_fts USING fts5(name, description);
INSERT INTO products_fts (name, description) VALUES ('Product 3', 'Description for product 3');
