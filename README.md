# multi-tour-worker-notifications

> Worker job 4: e-mail notifications (every domain)

Part of the **Multi Tour** distributed system — team `multi-tour`, Grupo 2.
Governance and documentation live in [`multi-tour-docs`](https://github.com/code-corhuila/multi-tour-docs).

## Branching

Three permanent branches. **None of them accepts a direct commit** — you enter through a child
branch and leave through a Pull Request.

```
develop  <--PR--  feat/... fix/... chore/...
qa       <--PR--  qa/...
main     <--PR--  release/...  hotfix/...
```

Promotion happens **by re-application** (`git cherry-pick -x`), never by merging one permanent
branch into another: `merge develop -> qa` and `merge qa -> main` do not exist in this model.

`main` requires **1 approval from `ariel5253`**. On `develop` and `qa` the team sets its own review
rule.

Full policy: `00-governance/branching-policy.md` in `multi-tour-docs`.

## What it does

Job 4 sends the business e-mail of the platform. It has no schedule: job 7 (`multi-tour-worker-event-relay`) delivers the notification events to its internal routes, and it turns each into an e-mail over SMTP (ADR-004 D3, D10). It has no database and calls no domain; a failed send never changes business state. The first notice is the one of a reset administrative password (HU-WRK-004, spec `001-hu-wrk-004-send-the-password-reset-notice` in `multi-tour-docs`). The second is the notice that an emergency access to a tenant opened or ended, one e-mail for each active Tenant Administrator (HU-SEC-007, event `EmergencyAccessNoticeRequested`, spec `002-hu-sec-007-send-the-emergency-access-notice` in `multi-tour-docs`). The third is the e-mail to the final customer about their reservation (HU-COM-015, event `ClientNotified`, eight types: proof approved or rejected, reservation confirmed, cancelled for non-payment, refund processed, credit balance recorded, departure cancelled, rescheduling proposed). That event carries no personal datum, so the worker reads the address, the name and the variables of the template at send time from `GET /api/v1/commercial-reservation/internal/tenants/{tenantId}/client-notices/{eventId}` with its `SERVICE` token; neither the content nor the address is logged. The record of the events already handled is kept in memory, so a restart forgets it.

## Variables

See `.env.example`; every variable is read at start and a bad one stops the worker naming it.

| Variable | Default | Meaning |
|---|---|---|
| `SMTP_HOST`, `SMTP_PORT` | none, required | The SMTP server |
| `SMTP_FROM` | none, required | The one platform address; the visible name of the sender is the tenant's |
| `SMTP_USER`, `SMTP_PASSWORD` | empty | Both or neither; the password is a secret, never versioned |
| `SMTP_REQUIRE_TLS` | `true` | Outside development the connection must be TLS; only Mailpit in development turns it off |
| `SMTP_TIMEOUT` | `10s` | Time allowed to one send |
| `HTTP_ADDR` | `:8080` | Where `/health` listens |
| `JWT_PUBLIC_KEY` or `JWT_PUBLIC_KEY_FILE` | none, one required | Public key of the security service (PEM; inline, line breaks as backslash-n; `deploy/compose.yml` passes only the inline one). The worker checks the token of every call itself and never holds the private key |
| `JWT_ISSUER`, `JWT_AUDIENCE` | `multi-tour-security`, `multi-tour` | Issuer and audience the token must carry |
| `COMMERCIAL_RESERVATION_BASE_URL`, `SERVICE_TOKEN` | empty | Both or neither. Where the content of a customer notice is read (scheme, host and port only) and the `SERVICE` token to read it; without them a `ClientNotified` event answers a failure and ends failed in job 7, visible. The token is a secret of the environment, never versioned |

## Development

Mailpit receives every e-mail and sends none. The root composition (`multi-tour-infra-postgres`) already declares it, with the profile `mail-capture`, for the security service; this worker's `deploy/compose.yml` does not declare a second one. Run the tests with `go test ./...`; the integration test against Mailpit runs only when `MAILPIT_URL` is set (the CI sets it).
