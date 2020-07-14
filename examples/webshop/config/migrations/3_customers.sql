-- Write your migrate up statements here

CREATE TABLE customers (
    id BIGSERIAL PRIMARY KEY,
    full_name character varying NOT NULL,
    phone character varying,
    stripe_id character varying,
    email character varying NOT NULL DEFAULT ''::character varying,
    encrypted_password character varying NOT NULL DEFAULT ''::character varying,
    reset_password_token character varying,
    reset_password_sent_at timestamp without time zone,
    remember_created_at timestamp without time zone,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);

-- Indices -------------------------------------------------------

CREATE UNIQUE INDEX index_customers_on_email ON customers(email text_ops);
CREATE UNIQUE INDEX index_customers_on_reset_password_token ON customers(reset_password_token text_ops);

---- create above / drop below ----

DROP TABLE customers;

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
