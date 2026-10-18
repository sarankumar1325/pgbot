# QueryShift TUI and database-change safety audit

Date: 2026-10-18

## Evidence

- User-provided screenshot at 2444 × 1638.
- Local PTY render at 130 × 40.
- Compact render tests at 80 × 24.
- Unit tests for connection gating, pending-command resume, write confirmation, and migration artifacts.
- Full repository tests, vet, and release binary build.

## Audited flow

1. **Application launch — healthy.** The QueryShift wordmark animation hands off to the workbench without a status-table splash.
2. **Disconnected home — healthy.** Conversation begins at the top and the single-line composer stays anchored at the bottom. The prior broken textarea block is gone.
3. **Slash command discovery — healthy.** Typing `/` opens a filtered, keyboard-selectable palette with descriptions and explicit locked/ready states.
4. **Database prerequisite — healthy.** `/schema`, `/erd`, `/inspect`, `/query`, and `/modernize` cannot run before a successful connection and catalog read. The selected command resumes automatically after connection.
5. **Connection input — healthy.** Bracketed paste reaches the masked input. The DSN stays in process memory and is not written to the migration journal.
6. **Read workspace — healthy in automated tests.** Schema, ER, inspection, and query routes use the existing read-only paths. No live external database was used during this audit.
7. **Write planning — healthy.** Writes remain locked behind `WRITE`; each statement is classified, previewed, and requires an exact per-change token.
8. **Migration versioning — healthy.** Before execution, QueryShift writes a timestamped SQL artifact, pre-change schema JSON, metadata, and `queryshift.sum` integrity manifest with owner-only file permissions. A post-change schema snapshot is added after commit.
9. **Failure behavior — healthy.** Execution runs as one PostgreSQL transaction with statement, lock, and idle-transaction timeouts. Failed execution rolls back the transaction and marks the migration failed.
10. **Destructive recovery — guarded, provider-dependent.** UPDATE, DELETE, DROP, TRUNCATE, and destructive ALTER require backup acknowledgement. The journal records evidence but cannot recreate lost row values; reliable post-commit recovery requires a verified provider backup or PITR point.

## Design changes

- Replaced the centered launcher with a top-to-bottom conversation layout.
- Replaced the multiline home textarea with a stable one-line composer; multiline SQL remains in the dedicated editor.
- Added a declarative command registry modeled on command-palette metadata: name, description, enabled state, and direct handler.
- Added compact palette pagination for 80 × 24 terminals.
- Made connection status and read/write state persistent in the header and composer.
- Added recovery classification to every change plan.

## Verification

```text
go test ./...  PASS
go vet ./...   PASS
make build     PASS
PTY 130×40     PASS
TUI 80×24      PASS
```

Screenshot-only review cannot prove screen-reader behavior, terminal color contrast across every theme, or recovery from a real provider backup. Those require dedicated accessibility testing and a disposable database restoration drill.
