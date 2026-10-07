# Configuration

Every setting is an environment variable. A bad or missing required setting stops the service at
start-up with every problem listed.

| Variable | Default | What it does |
| --- | --- | --- |
| `DATABASE_DSN` | required | Postgres connection string. |
| `MIGRATE_DSN` | `DATABASE_DSN` | A direct connection for the migrations, when the pool goes through a pooler. |
| `MIGRATIONS_DIR` | `migrations` | Where the SQL migrations are (`/migrations` in the image). |
| `RABBITMQ_URL` | required | The broker the audit outbox relay publishes to. |
| `GRPC_PORT` | `9090` | The gRPC listener. |
| `PROBE_PORT` | `8080` | Plain HTTP `/livez` and `/readyz`. |
| `IDENTITY_GRPC_ADDR` | `identity:9090` | steward-identity, asked who is an officer. |
| `REPORTING_OFFICER_GROUPS` | empty | The seed only: comma-separated local group ids or identity provider group names whose members work cases, matched ignoring case. It is stored in the Compliance settings on the first start against an empty database and ignored after that; change the officer groups through `SettingsService.UpdateSettings`. Seeded empty, nobody can open a case until a compliance admin names the groups; the service logs a warning. |
| `REPORTING_NOTICE_DAYS_AFFECTED` | `60` | Days allowed from discovery to notify the affected people. |
| `REPORTING_NOTICE_DAYS_REGULATOR` | `60` | Days allowed from discovery to notify a regulator. |
| `REPORTING_NOTICE_DAYS_MEDIA` | `60` | Days allowed from discovery to notify the media. |
| `REPORTING_NOTICE_DAYS_OTHER` | `60` | Days allowed from discovery for any other notice. |
| `REPORTING_PURGE_INTERVAL` | `1h` | How often the retention purge runs (a Go duration, at least `1m`), or `off` to pause it. |
| `WORKLOAD_OIDC_ISSUER` | required | The cluster's service-account token issuer (https). Without it the service won't start unless `WORKLOAD_AUTH=disabled`. |
| `WORKLOAD_OIDC_JWKS_URL` | discovered | Overrides the issuer's JWKS URL (https). |
| `WORKLOAD_OIDC_CA_FILE`, `WORKLOAD_OIDC_BEARER_FILE` | empty | A CA bundle and a bearer token for fetching the JWKS. |
| `WORKLOAD_AUDIENCE` | `steward` | The audience a caller's token must carry. |
| `WORKLOAD_ALLOWED_SERVICEACCOUNTS` | required with the issuer | Comma-separated `<namespace>/<serviceaccount>` that may call reporting at all: `steward/steward-gateway`. The per-method list is in code. |
| `WORKLOAD_TOKEN_FILE` | `/var/run/secrets/steward/token` | Reporting's own projected token, sent to identity on every call and re-read each time. A missing file stops the boot. |
| `WORKLOAD_AUTH` | enabled | `disabled` turns service-to-service authentication off, for local runs only: every caller is let in, a warning is logged every five minutes and readiness reports `workloadauth` degraded. Any other value is an error. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | Traces and metrics. |
| `LOG_LEVEL`, `LOG_FORMAT` | `info`, `json` | go-log. Local runs use `trace` and `console`. |

The notice days are counted from the case's discovery date, so changing a setting changes the
deadline of notices added after it; a notice keeps the days it was added with.

## Compliance settings

The officer groups, the public report link, the retention period after close and the intake
categories are not environment variables: they live in Postgres (`compliance_settings`) and are
changed through `SettingsService` ([API](api.md#settingsservice-the-compliance-settings-c10)). The
first start seeds them: officer groups from `REPORTING_OFFICER_GROUPS`, the public link on, 2555
days (seven years) of retention and no intake categories.

## Image

The Dockerfile takes `VERSION` (the image tag) and `COMMIT` (the full source SHA) and stamps them
into go-buildinfo with `-ldflags -X`. An unstamped build reports `dev` and commit `unknown`.

```sh
docker build --build-arg VERSION=v0.1.0 --build-arg COMMIT="$(git rev-parse HEAD)" -t steward-reporting .
```
