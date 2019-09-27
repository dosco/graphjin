-- Write your migrate up statements here

CREATE DATABASE hello_database

-- CREATE TABLE public.users (
--   id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
--   full_name   text
--   email       text UNIQUE NOT NULL CHECK (length(email) < 255),
--   encrypted_password text,
--   created_at timestamptz NOT NULL NOT NULL DEFAULT NOW(),
--   updated_at timestamptz NOT NULL NOT NULL DEFAULT NOW()
-- );

---- create above / drop below ----

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.

-- DROP TABLE public.users

DROP DATABASE IF EXISTS hello_database
