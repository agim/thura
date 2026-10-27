# Thura: Līdza v0.1.128 acceptance

Measured on this Mac, 2026-10-26, using a clean worktree of `origin/main`
(`dbe2083`). Thura has no master branch. No development server or other build
ran during these timings. Commands used `JOBS_WORKERS=0`, umask 077, local
PostgreSQL, and the same installed staticcheck/govulncheck on both versions.
The initial tool preflight found an incompatible global staticcheck and missing
govulncheck; those failed runs are excluded. No framework source was modified.

| Command / artifact | v0.1.125 | v0.1.128 |
| --- | ---: | ---: |
| First `lidza check --json` | 7.14 s | 6.80 s |
| Immediately repeated check | 5.04 s | 3.85 s |
| `lidza test` | 65.47 s | 66.05 s |
| Verify immediately after passing check/test | 67.32 s | 1.42 s |
| AGENTS.md | 27,023 bytes | 8,884 bytes |
| docs/lidza-guide.md | 153,159 bytes | 65,818 bytes |

The second updated check explicitly skips dependency scanners because dependencies
are unchanged; cargo deny has no Cargo project to scan. Both updated checks pass
without L024. The warm check is 24% faster, not three times faster on Thura.
Verify reuses both successful tree stamps. The integration package itself took
about 60 seconds before splitting; the 580-second figure was from Albaspot.

Migration audit: all 50 team notes occur verbatim exactly once in docs/notes.md;
all 32 recipe headings survive in docs/lidza-recipes.md, including both app-owned
recipes with unchanged bodies. Working agreements below the framework block are
unchanged. Thura had no custom bullets above that block. Dockerfile, SDK checksums,
agent files and recipe skills regenerated. The updater also normalized the test
PostgreSQL socket from /var/run/postgresql to /tmp on macOS. No application schema,
production configuration or convention changed; no decision entry is needed.
`.lidza/` and its verification/dependency stamps remain ignored.

A newly started stdio MCP server completed initialization plus tools/list in
0.0127 s (47 tools). `lidza_notes` with grep `dmarc` answered in 0.0004 s;
`lidza_routes` answered in 0.0377 s. Both returned successful tool results. This
checks an actual fresh server process, not a claim that an existing GUI client
was reconnected. Thura has no DMARC-specific team note.

Commit-hook reuse and the separate test-package split are measured below after
completion. Local raw command outputs are under `.lidza/upgrade-128/`.

The upgrade was committed separately as df5b605. The first commit took 0.03 s,
but inspection revealed this checkout had no core.hooksPath configured: that
number is not hook acceptance. The existing .githooks hook was enabled before the
subsequent test-layout commit; its measured result follows below.

The first full split run passed in 58.25 s (previously 66.05 s). Feature packages
ran in 0.9–6.7 s; the retained cross-service setup/SMIP package took 53.53 s and
still dominates a complete uncached run. Database inspection during the run
confirmed nine distinct package clones. An automated token audit confirms all
128 original tests remain once each with unchanged bodies, apart from qualifying
the shared fixture call. tests/README.md explains fixture isolation and scope.
