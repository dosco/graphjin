-- Write your migrate up statements here

CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    full_name character varying NOT NULL,
    phone character varying,
    avatar character varying,
    email character varying NOT NULL DEFAULT ''::character varying,
    encrypted_password character varying NOT NULL DEFAULT ''::character varying,
    reset_password_token character varying,
    reset_password_sent_at timestamp without time zone,
    remember_created_at timestamp without time zone,
    category_counts jsonb,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);

-- Indices -------------------------------------------------------

CREATE UNIQUE INDEX index_users_on_email ON users(email text_ops);
CREATE UNIQUE INDEX index_users_on_reset_password_token ON users(reset_password_token text_ops);


---- create above / drop below ----

DROP TABLE users;

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
