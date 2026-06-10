-- Runs once on first container start (fresh volume), connected to $POSTGRES_DB (realdeal_core).
--
-- Creates the read-only role used by the lookup service. The read-only boundary
-- is enforced by Postgres grants, not application convention: lookup_ro can
-- SELECT but never write. In AWS this maps to a read-only DB user on RDS (or a
-- read replica endpoint later).
--
-- Services that OWN their data get their own database here instead (e.g.
-- CREATE DATABASE realdeal_appointment) — see the "adding a service" recipe in
-- the README.

CREATE ROLE lookup_ro LOGIN PASSWORD 'lookup_ro_dev';
GRANT CONNECT ON DATABASE realdeal_core TO lookup_ro;
GRANT USAGE ON SCHEMA public TO lookup_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO lookup_ro;

-- Tables are created later by core's AutoMigrate (as user realdeal); make
-- future tables readable by lookup_ro automatically.
ALTER DEFAULT PRIVILEGES FOR ROLE realdeal IN SCHEMA public GRANT SELECT ON TABLES TO lookup_ro;
