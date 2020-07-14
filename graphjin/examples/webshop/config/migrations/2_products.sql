-- Write your migrate up statements here

CREATE TABLE products (
    id BIGSERIAL PRIMARY KEY,
    name character varying,
    description text,
    price numeric(7,2),
    user_id bigint REFERENCES users(id),
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL,
    tsv tsvector
);

-- Indices -------------------------------------------------------

CREATE INDEX index_products_on_tsv ON products USING GIN (tsv tsvector_ops);
CREATE INDEX index_products_on_user_id ON products(user_id int8_ops);

---- create above / drop below ----

DROP TABLE products;

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
