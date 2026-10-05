# steward-reporting 🐹

> 🧭 Compliance reporting service for Steward: anonymous and named reports, officer case work, breach assessment and notification deadlines

Anyone in the organisation can report a privacy or health-information concern, anonymously or with
their name, and a privacy officer works the case to a close.

- **Anonymous reports:** no name, email or address is kept. The reporter gets a one-time case code
  and chooses a passphrase, stored only as a slow hash; the two are the only way back to the
  report. Attachment metadata is stripped on the server.
- **Named reports:** a signed-in reporter sees their own reports and replies to them.
- **Officers:** the case queue, case detail, a two-way thread with the reporter, internal notes, a
  guided breach risk assessment ending in a recorded decision, notices with deadlines counted from
  discovery, and close-out with corrective actions that can link to a policy.
- Nothing in a case reaches anyone outside the officer group, and every view and change is audited.

It asks identity who is an officer and publishes steward-audit's `AuditEvent`.

## 🚀 Run

```bash
cp .env.example .env   # a local Postgres, RabbitMQ and identity
task run
```

Or build the image with `docker build --build-arg VERSION=dev --build-arg COMMIT=$(git rev-parse HEAD) -t steward-reporting .`.
Settings are in [configuration](docs/configuration.md); the probes are in the
[runbook](docs/runbook.md#probes).

## 📚 Docs

- [API](docs/api.md): the gRPC services, anonymity, the audit events and calling other services.
- [Configuration](docs/configuration.md).
- [Runbook](docs/runbook.md).
- [Error codes](docs/error-codes.md).

## 🛠 Develop

```bash
task build         # go build ./...
task test          # go test ./... (store, service and readiness tests start containers with testcontainers)
task lint          # gofmt check + golangci-lint + yamllint
task proto         # fetch the pinned callee protos, buf lint, regenerate gen/
task workloadauth  # compare internal/workloadauth with steward-core's copy
```

Set `DATABASE_TEST_DSN` to run the store and service tests against an existing Postgres instead of
a container.

## 🙏 Acknowledgements

Steward was originally written by [@Bugs5382](https://github.com/Bugs5382).

## ⚖️ License

Apache-2.0 (c) 2026 The Steward Authors
