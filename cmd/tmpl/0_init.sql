-- Write your migrate up statements here

CREATE TABLE users (
  id BIGINT NOT NULL PRIMARY KEY, 
  full_name VARCHAR(255) NOT NULL,
  email VARCHAR(255) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP
);

CREATE TABLE products (
  id BIGINT NOT NULL PRIMARY KEY, 
  name VARCHAR(255) NOT NULL,
  user_id BIGINT,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP,
  FOREIGN KEY (user_id) REFERENCES users(id)
);

---- create above / drop below ----

-- Write your down migrate statements here. If this migration is irreversible
-- then delete the separator line above.

DROP TABLE users

