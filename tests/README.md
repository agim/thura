# Integration tests

Feature packages run concurrently through `lidza test`: assistant, calendar,
chat, contacts (including Loop), Drive/Office, mail, Meet and workspace. Each
uses Līdza's package database clone. `testsupport.StartConfigured` is the shared
published-installation fixture; it preserves caller-selected storage and gives
other fixtures temporary private storage.

The root package retains the cross-service setup/SMIP scenarios and their shared
multi-server fixtures. Its TestMain creates a package clone for ordinary app
fixtures and opts out of automatic URL replacement only in that process: setup
and SMIP fixtures must keep their independently created databases. All other
packages retain automatic isolation. Never connect concurrent tests to the
migrated template itself. `lidza test` removes the package clones at completion.

Do not use `t.Parallel` inside a package: these fixtures change process-wide
working directory, environment and credentials. To limit machine load use
`lidza test -p 4`. Change a feature package independently to reuse Go's results
for untouched packages; changes to shared support or app wiring affect them all.
