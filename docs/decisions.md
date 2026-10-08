# Decisions

What shaped thura and why, one entry each, newest last: a pack added, Rust
chosen for a module, a dependency taken, a schema tradeoff, an
integration. Read this before working in those areas. Record yours in the
same commit as the change: `lidza decision add "Title" --why "..."`
(MCP: `lidza_decision_add`). A recipe in docs/lidza-guide.md says how
this app does something; a decision here says why it is done that way.

## Reuse the uploaded prototype behind a preview boundary

Preserve the supplied six-app interactions while making the persistence boundary explicit. Imported JavaScript stays in `src/workspace` with ESLint enabled; `allowJs` supports an incremental TypeScript conversion. New product pages are strict TypeScript and use generated API clients. Lucide React 1.52.0 supplies the prototype's icons. The preview has its own local-storage namespace and a persistent sample-data banner.

## Invite-only account and contact foundation

Use the published Līdza auth/db packs. The developer chose invite-only workspaces, workspace-owned data, and owner/admin/member roles. Public registration and external providers are disabled; local password accounts are an explicitly provisional implementation choice. Initial account/workspace creation uses operator MCP tools. Contacts are workspace-scoped in SQL and each request checks current membership, so a session cannot retain revoked access. Invitation acceptance, membership management, and production mail will follow in later slices.

## 2026-10-08: Who owns the data: Each workspace owns its data; access is limited to its members and ex…

Why: Answered in the brief interview (docs/brief.md): Each workspace owns its data; access is limited to its members and explicit grants. Every query is scoped by it; the most expensive answer to change later.

Touches: docs/brief.md

## 2026-10-08: Pack db added

Why: Persist workspace membership and contacts in Postgres; auth uses the same database.

Touches: lidza.json, packs.go
