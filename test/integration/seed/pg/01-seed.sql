-- dbcli pg/kingbase integration seed (runs in database `bizdb`).
-- Multiple schemas (the pg "namespace" level, surfaced as `database` in dbcli),
-- including a same-named table in two schemas to exercise the multi-schema
-- duplication + describe-ambiguity handling. Plus edge-case rows + scoped roles.

CREATE SCHEMA IF NOT EXISTS sales;

CREATE TABLE public.users (
  id         BIGINT PRIMARY KEY,
  name       TEXT NOT NULL,
  bio        TEXT,
  avatar     BYTEA,            -- binary → base64 in output
  big        BIGINT,           -- >2^53 precision
  created_at TIMESTAMPTZ
);
COMMENT ON TABLE public.users IS 'application users';

INSERT INTO public.users (id, name, bio, avatar, big, created_at) VALUES
  (1, 'Alice', repeat('x', 300), '\xfffe0001'::bytea, 9007199254740993, '2026-06-27T10:00:00Z'),
  (2, 'Bob',   'short bio',      NULL,                 42,               '2026-06-27T11:00:00Z'),
  (3, 'Carol', NULL,             NULL,                 7,                NULL);

CREATE TABLE public.orders (id BIGINT PRIMARY KEY, user_id BIGINT, status TEXT, amount NUMERIC(10,2));
INSERT INTO public.orders VALUES (1,1,'PAID',100.50), (2,2,'NEW',250.00), (3,1,'SHIPPED',75.25);

-- same table NAME in a second schema → (schema,name) is the key, describe of a
-- bare `orders` must report ambiguity.
CREATE TABLE sales.orders (id BIGINT PRIMARY KEY, region TEXT, total NUMERIC(10,2));
INSERT INTO sales.orders VALUES (1,'EU',500.00), (2,'US',320.00);

-- Read-only role across both schemas (primary boundary).
CREATE ROLE app_ro LOGIN PASSWORD 'ro_pw';
GRANT USAGE ON SCHEMA public, sales TO app_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA public, sales TO app_ro;

-- Read-write role for --allow-write tests (public schema).
CREATE ROLE app_rw LOGIN PASSWORD 'rw_pw';
GRANT USAGE ON SCHEMA public TO app_rw;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO app_rw;
