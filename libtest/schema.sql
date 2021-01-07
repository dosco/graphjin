USE db;

CREATE TABLE users (
  id INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  email VARCHAR(50) NOT NULL,
  UNIQUE (email)
);

CREATE TABLE products (
  id INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(50),
  user_id INT,
  FOREIGN KEY (user_id) REFERENCES users(id)
);

INSERT INTO users(id, email) values(1, 'user1@test.com');
INSERT INTO users(id, email) values(2, 'user2@test.com');
INSERT INTO users(id, email) values(3, 'user3@test.com');

INSERT INTO products(id, name, user_id) values(1, 'IPhone 11', 1);
INSERT INTO products(id, name, user_id) values(2, 'Google Pixel', 2);
INSERT INTO products(id, name, user_id) values(3, 'Moto G', 3);