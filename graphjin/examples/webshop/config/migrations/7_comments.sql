-- Write your migrate up statements here

CREATE TABLE comments (
    id BIGSERIAL PRIMARY KEY,
    body text CHECK (length(body) > 1 AND length(body) < 200),

    product_id     bigint REFERENCES products(id),
    user_id         bigint REFERENCES users(id),
    reply_to_id     bigint REFERENCES comments(id),

    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);

---- create above / drop below ----

DROP TABLE comments;

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
