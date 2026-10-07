# Runbook

## Start-up

The service applies the baseline migration and the audit outbox's own migration, connects to
Postgres and RabbitMQ, starts the audit relay, dials identity and serves gRPC. A bad setting stops
it with every problem listed. On an empty database it seeds the Compliance settings from
`REPORTING_OFFICER_GROUPS`; seeded with no officer groups it starts, warns, and refuses every case
call until a compliance admin names them. Later starts log that the stored settings are kept.

## Probes

Readiness follows go-buildinfo's dependency checker. Each check has a 2-second timeout, and a result
is reused for 5 seconds.

| Dependency | Required | When it's down |
| --- | --- | --- |
| `postgres` | yes | Not ready: nothing can be read or written. |
| `rabbitmq` | yes | Not ready: no audit event is relayed. |
| `workloadauth` | yes, with authentication on | Not ready: no caller's token can be checked. With `WORKLOAD_AUTH=disabled` it is reported degraded. |
| `identity` | no | Degraded, still ready: anonymous reports keep coming in and can be checked; officer and named-report calls answer `IDENTITY_UNAVAILABLE` or fail. |

- **HTTP on `PROBE_PORT` (8080):** `GET /livez` is 200 while the process is up and never checks a
  dependency. `GET /readyz` is 200 while ready and 503 while a required dependency is down; its
  JSON body lists every dependency with its state and error class. There's no plain `/health`.
- **gRPC on `GRPC_PORT`:** `grpc.health.v1` with the service name `liveness` reports the process
  only; the empty name and `readiness` follow readiness. Every `Health/Check` answer carries
  `steward-version`, `steward-commit`, `steward-dep-postgres` and `steward-depstate-<name>`. Health
  needs no token.
- Never point liveness at a dependency: an outage would restart every replica.
- Readiness recovers on its own once the dependency is back.

## Retention purge

Every replica runs the purge every `REPORTING_PURGE_INTERVAL` (hourly by default, and once at
start-up), but only one purges at a time: each batch of up to 100 cases runs in a transaction
holding the advisory lock `steward-reporting:retention-purge`, and a replica that can't take it
skips the run. A closed case is purged once the retention period in the Compliance settings
(`GetSettings`, seven years by default) has passed since it closed. Open cases and held cases are
never purged.

- **Read it:** each run logs `retention purge done` with the count, the retention days, the cutoff
  and the duration, and each purged case logs `case purged` with its id. The audit log holds a
  `case.purged` event per case. A failed run logs `retention purge failed` and retries on the next
  interval.
- **Keep one case:** place a legal hold on it (`LegalHoldService.PlaceLegalHold`); release the hold
  to let the purge take it.
- **Pause it:** set `REPORTING_PURGE_INTERVAL=off` and restart. Every replica then logs a warning at
  start-up. Raising the retention period in the settings also keeps cases longer, from the next run.

## Common problems

| Symptom | Look at |
| --- | --- |
| `CASE_ACCESS_DENIED` for an officer | The officer groups in the Compliance settings (`GetSettings`; `REPORTING_OFFICER_GROUPS` is only the first seed), and the user's groups in identity: a local group id or an identity provider group name, matched ignoring case. A disabled or deleted account is never an officer. |
| `PUBLIC_LINK_OFF` on an anonymous report | The public link is switched off in the Compliance settings. |
| `REPORT_INVALID` with field `category` | The report's intake category isn't one configured in the settings, or one is required. |
| `REPORT_SIGN_IN_REQUIRED` on every named or case call | The gateway isn't passing the user: check its service account is `steward/steward-gateway` and listed in `WORKLOAD_ALLOWED_SERVICEACCOUNTS`. |
| `Unauthenticated` or `PermissionDenied` with no error code | Service-to-service authentication refused the call; `reporting.call.refused` audit events say who and why. |
| `Unavailable: workload verifier unavailable` | The JWKS hasn't loaded, so every call that needs a token is refused and `workloadauth` reports down until a fetch succeeds. A `status 401` in the `JWKS refresh failed` log line means the API server refused `WORKLOAD_OIDC_BEARER_FILE`: it must hold a token with the API server's own audience, not the `steward` caller token. |
| `Code 8101: Internal Error` | A store call failed; the log line with the same trace id names the `op`. |
| Audit events stop arriving | `SELECT status, count(*) FROM audit_outbox GROUP BY status`: `pending` rows mean the relay can't publish (check RabbitMQ), `dead` rows keep their last error. |
| Many `report.check_refused` events | Someone is guessing case codes. Throttle at the gateway; each try already costs one argon2id hash. |

## Privacy

- Logs carry case ids, officer ids and counts, never report text, notes, messages, passphrases,
  case codes or request peers. Keep it that way when adding log lines.
- An anonymous case has no reporter id; nothing in this service can tell who filed it. Don't add
  request metadata (forwarding headers, user agents) to logs or traces.

## Backups

Back up Postgres. The cases, their threads, notes and attachments live only here; the audit chain
in steward-audit holds who viewed or changed them.
