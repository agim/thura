# thura

A Līdza application: a Go control plane, a react frontend, one
binary. `docs/lidza-guide.md` is the guide, `docs/decisions.md` says why
the app is built the way it is.

The first implementation integrates the supplied six-app prototype at
`/workspace` and provides persistent shared Mail, Contacts, Drive, and Calendar at `/app`.
See [implementation status](docs/implementation-status.md) for account
provisioning, validation, and the remaining features.

## Run

```sh
lidza install    # after cloning or pulling: .env, databases, node_modules
lidza update     # the framework to its newest release, then lidza install
lidza dev        # http://127.0.0.1:3000, hot reload for Go and the frontend
lidza test       # Go tests; lidza test --e2e for the browser suite
lidza check      # every Go, Rust and frontend error in one list
```

`.github/workflows/ci.yml` runs `lidza verify --strict` (warnings fail),
govulncheck, `npm audit` on the runtime packages and the browser suite on every push. `.github/dependabot.yml` proposes
dependency updates weekly; the framework moves with `lidza update`.

Settings live in `.env` (copied from `.env.example`); secrets in the
credentials (`lidza credentials set NAME=value`), sealed with
`config/master.key`.

## Build with an agent

`claude`, `codex` or `gemini` in this directory: the MCP server and the
recipes are configured (`AGENTS.md`, which `CLAUDE.md` and `GEMINI.md`
import).

## Deploy

```sh
lidza ship --domains app.example.com --email ops@example.com
```

builds `bin/thura` after the checks and writes `deploy/production.env`.
The process needs that file plus `DATABASE_URL` and `LIDZA_MASTER_KEY`;
`deploy/thura.service` runs it under systemd, the `Dockerfile` in a
container. With the domains set the binary serves HTTPS itself. Admins
are named in `ADMIN_USERS` and manage providers at `/admin`.
