-- Write your migrate up statements here

CREATE TABLE public.users (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  full_name  text,
  email      text UNIQUE NOT NULL CHECK (length(email) < 255),
  created_at timestamptz NOT NULL NOT NULL DEFAULT NOW(),
  updated_at timestamptz NOT NULL NOT NULL DEFAULT NOW()
);

---- create above / drop below ----

-- Write your down migrate statements here. If this migration is irreversible
-- then delete the separator line above.

DROP TABLE public.users

