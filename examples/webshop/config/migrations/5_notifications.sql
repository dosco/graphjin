-- Write your migrate up statements here

CREATE TABLE notifications (
    id BIGSERIAL PRIMARY KEY,
    key character varying,
    subject_type character varying,
    subject_id bigint,
    user_id bigint REFERENCES users(id),
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);

-- Indices -------------------------------------------------------

CREATE INDEX index_notifications_on_subject_id ON notifications(subject_id int8_ops);
CREATE INDEX index_notifications_on_user_id ON notifications(user_id int8_ops);

---- create above / drop below ----

DROP TABLE notifications;

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
