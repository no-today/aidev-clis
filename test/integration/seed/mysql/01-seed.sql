-- dbcli mysql integration seed.
-- Two databases (the mysql "namespace" level), edge-case rows, and scoped
-- read-only + read-write users (the real security boundary under test).

CREATE DATABASE IF NOT EXISTS app;
CREATE DATABASE IF NOT EXISTS billing;

USE app;

CREATE TABLE users (
  id         BIGINT PRIMARY KEY,
  name       VARCHAR(64) NOT NULL,
  bio        TEXT,
  avatar     BLOB,
  big        BIGINT,             -- exercises >2^53 JS-precision handling
  created_at DATETIME,
  KEY idx_name (name)
) COMMENT='application users';

INSERT INTO users (id, name, bio, avatar, big, created_at) VALUES
  (1, 'Alice', REPEAT('x', 300), 0xFFFE0001, 9007199254740993, '2026-06-27 10:00:00'),
  (2, 'Bob',   'short bio',       NULL,        42,               '2026-06-27 11:00:00'),
  (3, 'Carol', NULL,              NULL,        7,                NULL);

CREATE TABLE orders (
  id       BIGINT PRIMARY KEY,
  user_id  BIGINT,
  status   VARCHAR(32),
  amount   DECIMAL(10,2)
);
INSERT INTO orders VALUES
  (1, 1, 'PAID', 100.50),
  (2, 2, 'NEW', 250.00),
  (3, 1, 'SHIPPED', 75.25);

USE billing;
CREATE TABLE invoices (id BIGINT PRIMARY KEY, order_id BIGINT, paid TINYINT(1));
INSERT INTO invoices VALUES (1, 1, 1), (2, 3, 0);

-- Read-only account: SELECT only, across both databases. This is the primary
-- boundary — even a guard bypass cannot write through it.
CREATE USER 'app_ro'@'%' IDENTIFIED BY 'ro_pw';
GRANT SELECT ON app.* TO 'app_ro'@'%';
GRANT SELECT ON billing.* TO 'app_ro'@'%';

-- Read-write account for --allow-write tests (app db only).
CREATE USER 'app_rw'@'%' IDENTIFIED BY 'rw_pw';
GRANT SELECT, INSERT, UPDATE, DELETE ON app.* TO 'app_rw'@'%';

FLUSH PRIVILEGES;
