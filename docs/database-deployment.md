# Database deployment and switching

This guide is for operators with access to the host's deployment configuration. For a new Docker Compose installation, the default file starts A1O Platform and PostgreSQL 17. Redis is optional. A direct binary or container launch still uses SQLite when `SQL_DSN` is empty, so existing standalone installations keep using their database.

## Choose a deployment

Run Compose commands from the same project directory. Use the same Compose project name as before if you set one explicitly.

| Database | Configuration | Start or apply configuration |
| --- | --- | --- |
| Bundled PostgreSQL | Set a strong `POSTGRES_PASSWORD` in the project's environment or `.env` before first launch. Retain it on later launches. PostgreSQL data is mounted from `./data/postgres`. The database is reachable only on the Compose network by default. | `docker compose up -d` |
| Existing PostgreSQL | Set `SQL_DSN` to a `postgres://` URL for an existing application database and a role permitted to run schema migrations. Use verified TLS for remote database connections and URL-encode credentials in the URL. | `docker compose -f docker-compose.external.yml up -d` |
| Existing MySQL | Set `SQL_DSN` to a MySQL DSN for an existing application database and a user permitted to run schema migrations. | `docker compose -f docker-compose.external.yml up -d` |
| SQLite | Leave `SQL_DSN` empty. With the default SQLite path, its database file is in `./data/a1o-platform`. Keep any custom `SQLITE_PATH` inside the mounted application data directory and continue using the same path. | `docker compose -f docker-compose.external.yml up -d` |
| Previously bundled MySQL and Redis | Keep the existing credentials and `./data/mysql`. The legacy file retains the service names and mounts. | `docker compose -f docker-compose.mysql.yml up -d` |

The default Compose file always connects the app to its bundled PostgreSQL. To use an external service, choose the external file explicitly. Set `REDIS_CONN_STRING` only if connecting to an existing Redis service. Use the same Compose file for future app updates and configuration changes. If the deployment uses older `one-api` names or paths, retain its existing configuration and storage locations until they have been backed up and migrated separately.

## Deploy a prebuilt image from GHCR

The public image is published for `linux/amd64`. Use the Compose files from a checkout that supports `A1O_IMAGE`. For a new installation, copy `.env.example` to `.env` and configure the database and session secret as described in the README. For an existing installation, keep its Compose project name, database configuration, and data directories.

1. Set `A1O_IMAGE=ghcr.io/infinmalum/a1o-platform:v0.1.0-beta.3` in the project’s `.env` to pin this release. Use `ghcr.io/infinmalum/a1o-platform:latest` to follow the latest published image, which can be a Beta.
2. Pull and start the pinned image with bundled PostgreSQL. Use the same reference in `docker pull` that you set in `.env`.

   ```sh
   docker pull ghcr.io/infinmalum/a1o-platform:v0.1.0-beta.3 && docker compose up -d --no-build
   ```

   For SQLite or an external database, use `docker compose -f docker-compose.external.yml up -d --no-build` after a successful pull. For existing bundled MySQL deployments, use `docker compose -f docker-compose.mysql.yml up -d --no-build` after a successful pull.

3. Verify `docker compose ps` with the same Compose file, then open `http://localhost:3000/admin/setup` for a new site or sign in to the existing site.

For later image updates, edit `A1O_IMAGE` if you use a pinned tag, pull the chosen reference, and repeat the same Compose command with `--no-build`. Leave `A1O_IMAGE` unset to use the source build instead. On other architectures, build from source or use the matching binary from the release page.

## Switch an existing installation

**First decide whether the target already contains the site's data.** For an upgrade that keeps the same database, select the matching deployment above. For a separate, empty installation, use a separate deployment directory and application data mount. Changing `SQL_DSN` only changes the connection. The application's startup migrations create or update schema in that database and do not copy data from another one.

For a switch that must preserve data, the operator should:

1. Schedule a maintenance window and stop writes. Stop all application instances and workers, keeping the old database available for backup and transfer. Record any unsettled online payments and pending media or Work jobs.
2. Back up the original database, application data directory, and deployment configuration, including the credentials and encryption key versions the application needs to read existing records. Back up the separate log database as well if `LOG_SQL_DSN` is set. Keep the previous containers' configuration and data paths available for recovery.
3. Transfer and verify data in a separate target database using tools appropriate to the source and target engines. A cross-engine transfer requires schema, data type, and ID sequence checks. Keep the old database and files intact until verification is complete.
4. Start the app with the selected Compose file or updated environment. Before opening traffic, verify existing administrator login, user balances and orders, sample conversations, media files, and the next generated record IDs. Reconcile payments that settled during the maintenance window against the gateway records.
5. If any check fails, keep writes paused and restore the previous connection and application configuration. If the target has already accepted writes, reconcile them before switching back to avoid losing newer records.

These deployment modes use the existing database engines and application data paths. They do not include an admin-console database switch, live connection replacement, or an automatic cross-engine data transfer.
