---
name: run-tests
description: Run the project test suite. Use when the user wants to verify a change.
argument-hint: "[package-or-path]"
---

Run the project tests.

If $ARGUMENTS is provided, scope to that package or path. Otherwise run the
full suite. For Go projects use `go test ./...`. For JS/TS use the relevant
package-manager test script (npm test / pnpm test / yarn test).

Summarize pass/fail counts and show the first 3 failures in full.
