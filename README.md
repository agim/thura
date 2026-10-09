# thura

A Līdza application: a Go control plane, a react frontend, one
binary. `docs/lidza-guide.md` is the guide, `docs/decisions.md` says why
the app is built the way it is.

The first implementation integrates the supplied six-app prototype at
`/workspace` and provides persistent Mail, Contacts, Drive, Calendar, Matrix Chat, and LiveKit Meet at `/app`, with optional ONLYOFFICE editing.
Mail defaults to Debian Postfix with operator-selected hosts and domains; see [Postfix deployment](docs/postfix.md). Drafts autosave, and Mail/Contacts search and paging cover the full collection.
See [implementation status](docs/implementation-status.md) for account
provisioning and validation; [operations](docs/operations.md) covers quotas, recovery, and deployment gates.

## Run

```sh
lidza install    # after cloning or pulling: .env, databases, node_modules
lidza dev        # http://127.0.0.1:3000, hot reload for Go and the frontend
lidza test       # Go tests; lidza test --e2e for the browser suite
lidza check      # every Go, Rust and frontend error in one list
```

`.github/workflows/ci.yml` runs `lidza verify --strict` (warnings fail),
govulncheck, `npm audit` on the runtime packages and the browser suite on every push. `.github/dependabot.yml` proposes
dependency updates weekly; the Debian Postfix relay/SMTP recovery fixture also runs in CI; the framework moves with `lidza update`.

Settings live in `.env` (copied from `.env.example`); secrets in the
credentials (`lidza credentials set NAME=value`), sealed with
`config/master.key`.

## Build with an agent

`claude`, `codex` or `gemini` in this directory: the MCP server and the
recipes are configured (`AGENTS.md`, which `CLAUDE.md` and `GEMINI.md`
import).

## Deploy

Follow [Install and configure a Thura server](docs/install-server.md) for services, DNS/MX, Mailgun or SMTP, private storage, first-administrator setup and user invitations. Existing deployments must apply pending migrations before restarting.

```sh
lidza ship --domains "$THURA_APP_DOMAIN" --email "$THURA_OPERATOR_EMAIL"
```

Set those variables to the deploying operator's chosen domain/email. The command builds `bin/thura` after the checks and writes `deploy/production.env`.
The process needs that file plus `DATABASE_URL` and `LIDZA_MASTER_KEY`;
`deploy/thura.service` runs it under systemd, the `Dockerfile` in a
container. With the domains set the binary serves HTTPS itself. New servers create their first administrator at `/setup` and configure providers
in the resumable `/admin/setup` wizard. Existing servers authorize operator
accounts explicitly through `ADMIN_USERS`.
