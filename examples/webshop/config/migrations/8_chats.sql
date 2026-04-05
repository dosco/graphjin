-- Write your migrate up statements here

CREATE TABLE chats (
    id BIGSERIAL PRIMARY KEY,
    body text,

    reply_to_id     bigint[],

    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);

---- create above / drop below ----

DROP TABLE chats;

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
