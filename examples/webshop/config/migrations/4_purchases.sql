-- Write your migrate up statements here

CREATE TABLE purchases (
    id BIGSERIAL PRIMARY KEY,
    customer_id bigint REFERENCES customers(id),
    product_id bigint REFERENCES products(id),
    quantity integer,
    returned_at timestamp without time zone,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);

-- Indices -------------------------------------------------------

CREATE INDEX index_purchases_on_customer_id ON purchases(customer_id int8_ops);
CREATE INDEX index_purchases_on_product_id ON purchases(product_id int8_ops);

---- create above / drop below ----

DROP TABLE purchases;

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
