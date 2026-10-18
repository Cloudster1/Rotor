#!/bin/sh
# Runs once, on first initialisation: rotor_app is bound by every RLS policy, rotor_admin is exempt by policy
# rather than by being superuser. The foundation migration creates both without a password when they are missing.
set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-SQL
    CREATE ROLE rotor_app LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD '${APP_DB_PASSWORD:-rotor_app}';
    CREATE ROLE rotor_admin LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD '${ADMIN_DB_PASSWORD:-rotor_admin}';
    GRANT CONNECT ON DATABASE "$POSTGRES_DB" TO rotor_app, rotor_admin;
SQL
