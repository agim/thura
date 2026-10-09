#!/usr/bin/env bash
# Staticcheck 2026.2.1's original x/tools cannot decode Go 1.27.2 export data.
# Build the published analyzer with the published reader fix in an isolated
# module. This never changes the application's dependency graph.
set -euo pipefail
THURA_ANALYSIS_BUILD=$(mktemp -d "${TMPDIR:-/tmp}/thura-analysis.XXXXXX")
trap 'rm -rf "$THURA_ANALYSIS_BUILD"' EXIT
cd "$THURA_ANALYSIS_BUILD"
export GOWORK=off
export GOFLAGS='-mod=mod -p=4'
go mod init thura.local/analysis-tools
go get honnef.co/go/tools/cmd/staticcheck@v0.8.1 golang.org/x/tools@v0.51.0
go install honnef.co/go/tools/cmd/staticcheck
