# realdeal-api

Go monorepo for RealDeal's backend services. One codebase, multiple deployable
services: each `cmd/<service>/` directory builds its own container image from
the shared `Dockerfile`, runs as its own compose service locally, and maps to
its own ECS service in AWS.

## Services

| Service | Path prefix | Data access | Purpose |
|---|---|---|---|
| `core` | `/api/v1/*` (default) | owns `realdeal_core` (read/write, runs migrations) | auth, users, properties, offers, favorites, upload presign |
| `lookup` | `/api/v1/search/*` | `realdeal_core` via `lookup_ro` (SELECT-only DB user) | listing search & browse |

## Local development

The compose stack is a 1:1 model of the future AWS deployment:

| Local | AWS |
|---|---|
| nginx `gateway` (:8080) | ALB + listener rules |
| each compose service | an ECS Fargate service |
| `postgres` (one instance, DB per owning service) | RDS |
| `localstack` (S3, SecretsManager) | S3 + CloudFront, SecretsManager |

Same code path everywhere — only config differs (`AWS_ENDPOINT_URL`, DB host,
credentials). See `realdeal-infra` for the CloudFormation side.

```sh
make dev               # fast iteration: postgres+localstack in Docker, core via go run
make up                # full containerized stack behind the gateway on :8080
make test              # unit tests
make test-integration  # bring up full stack, run HTTP flows through the gateway, tear down
make smoke             # run the same integration tests against API_BASE_URL (e.g. the real ALB)
```

First run after the multi-service migration: the Postgres volume must be
recreated so the init script runs (`docker compose down -v`).

## Adding a service

1. **Entrypoint** — create `cmd/<name>/main.go` (copy `cmd/lookup/main.go` as the
   skeleton). Shared packages (`internal/config`, `internal/middleware`,
   `internal/database`, `internal/handlers`) are reused, not duplicated.
2. **Data** — in `scripts/postgres-init.sql`:
   - service **owns** its data → `CREATE DATABASE realdeal_<name>` and connect
     with the `realdeal` user (use `database.Connect`, which runs AutoMigrate);
   - service is a **read side** over another service's data → create a
     SELECT-only role like `lookup_ro` (use `database.ConnectReadOnly`).
3. **Container** — add a compose service in `docker-compose.yml` with
   `build.args.SERVICE: <name>` and `profiles: ["full"]`. No new Dockerfile.
4. **Routing** — add an upstream + `location` block in `gateway/nginx.conf`.
   Each block corresponds to a future ALB listener rule; keep them in sync.
5. **Tests** — unit tests beside the handlers; end-to-end flows in
   `tests/integration/` (they run through the gateway, so they exercise the
   routing too).

Postgres init scripts only run on a fresh volume — `docker compose down -v`
after changing `scripts/postgres-init.sql`.
