-- Write your migrate up statements here

CREATE TABLE categories (
  id BIGSERIAL PRIMARY KEY,

  name          text NOT NULL           CHECK (length(name) < 100),
  description   text                    CHECK (length(description) < 300),

  created_at timestamp without time zone NOT NULL,
  updated_at timestamp without time zone NOT NULL
);

---- create above / drop below ----

DROP TABLE categories

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
