# Pre-refactor backup fixture generator

`prerefactor_backup.pcb` (in the parent `testdata/` directory) is an encrypted
panel backup produced by the pre-refactor baseline commit `de6a681`. It lets
`internal/platform/server/prerefactor_backup_test.go` prove that a backup taken
before the Phase 9 receipt-archive removal still restores on current code.

Regenerate it only if the fixture is lost or the test data must change, and
always from a worktree at `de6a681` that has the **original bytes** of
`migrations/0004_deployment.sql` and `migrations/0036_agent_updates.sql`.

Those two migrations were rewritten with CRLF when the baseline was imported,
which changed their SHA-256. `db.Open` and backup validation compare the
embedded migration bytes against `schema_migrations.checksum`, so a baseline
that still holds the CRLF bytes cannot produce a fixture that restores. See
`docs/lightweight-refactor-plan.md`, "Finding during Phase 9: migration bytes
changed at baseline import".

## Steps

Run from the main repository root (`/path/to/qui`):

```sh
# 1. Create a detached worktree at the baseline commit.
git worktree add --detach /path/to/qui-baseline de6a681

# 2. Copy the original migration bytes in (0004 and 0036 only).
cp migrations/0004_deployment.sql migrations/0036_agent_updates.sql /path/to/qui-baseline/migrations/

# 3. Confirm they match migrations/checksums_test.go:
#    0004_deployment.sql  = ba7844bdf512fe40916f44976280e061fe4f3f2177c5c373cac04f46328bc55c
#    0036_agent_updates.sql = f176741d723494a046bd28649297fc9ca2b500e1480bf605132c4661ca81d698

# 4. Copy this generator into the baseline server package.
cp internal/platform/server/testdata/generate_prerefactor/generate_prerefactor_fixture_test.go \
   /path/to/qui-baseline/internal/platform/server/

# 5. Run it with the shared toolchain and write the fixture back into this tree.
export PATH="/path/to/qui/.tools/go/bin:$PATH" \
       GOCACHE="/path/to/qui/.tools/gocache" \
       GOPATH="/path/to/qui/.tools/gopath" \
       GOTOOLCHAIN=local
PREREFACTOR_FIXTURE_OUT="/path/to/qui/internal/platform/server/testdata/prerefactor_backup.pcb" \
  go test ./internal/platform/server -run '^TestGeneratePreRefactorFixture$' -count=1 -v
# (run the go test from inside /path/to/qui-baseline)

# 6. Remove the worktree.
git worktree remove --force /path/to/qui-baseline
```

Without `PREREFACTOR_FIXTURE_OUT` the generator skips, so it is harmless to
compile. This copy lives under `testdata/`, which the Go tool ignores entirely;
it is kept only as the documented source of the fixture. The generator boots a
server, creates an Owner, creates a user, node group and policy through the
real HTTP API, inserts one authenticated `access_receipt_archives` row and one
`user_node_budgets` row (with its version) through SQL, then creates a backup
through the backups API with password `fixture-backup-password`.
