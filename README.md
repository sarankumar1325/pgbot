
> **Derived work.** This repository is a copy of
> [`pgrundev/pgbot`](https://github.com/pgrundev/pgbot),
> used under the Apache License 2.0 — `Copyright 2026 pgrun.dev`.
> See [`LICENSE`](LICENSE).
> QueryShift adds the Bedrock-powered multi-agent modernization workflow and
> rebrands the inherited read-only PostgreSQL diagnostics engine.

<p align="center">
  <img src="docs/img/queryshift-mark.png" alt="" width="82" height="82">
</p>

<h1 align="center">QueryShift</h1>

<p align="center">
  <strong>Evidence-backed PostgreSQL change intelligence.</strong><br>
  Ask, observe, propose, rehearse, approve, apply, and verify database changes
  through one credential-safe workflow.<br>
  Deterministic tools provide evidence; models explain and propose while
  policy-enforced code remains the execution boundary.
</p>

<p align="center">
  <a href="https://github.com/sarankumar1325/queryshift/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/sarankumar1325/queryshift/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/sarankumar1325/queryshift/releases/latest"><img alt="Release" src="https://img.shields.io/github/v/release/sarankumar1325/queryshift"></a>
  <a href="https://pkg.go.dev/github.com/sarankumar1325/queryshift"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/sarankumar1325/queryshift.svg"></a>
  <a href="https://goreportcard.com/report/github.com/sarankumar1325/queryshift"><img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/sarankumar1325/queryshift"></a>
  <a href="LICENSE"><img alt="License: Apache-2.0" src="https://img.shields.io/badge/license-Apache--2.0-blue"></a>
  <img alt="PostgreSQL 14–18" src="https://img.shields.io/badge/postgres-14%E2%80%9318-336791">
</p>

<p align="center">
  <a href="#quickstart">Quickstart</a> ·
  <a href="#install">Install</a> ·
  <a href="#setup--a-read-only-role-with-pg_monitor">Setup</a> ·
  <a href="#commands-and-flags">Commands</a> ·
  <a href="#ci-integration">CI</a> ·
  <a href="#mcp--use-queryshift-as-an-agent-tool">MCP</a> ·
  <a href="#the---json-contract">JSON contract</a> ·
  <a href="#troubleshooting">Troubleshooting</a> ·
  <a href="docs/providers.md">Provider notes</a>
</p>

> **Status: beta.** The `--json` contract is versioned (currently `1.5.0`, JSON
> Schema published in [`schema/`](schema/)) and breaking changes to it are
> treated as breaking changes to the tool. The human-readable report is **not**
> a stable interface — parse `--json`, not the terminal output.

---

## Quickstart

Build and open the interactive terminal app:

```sh
go build -o ./bin/queryshift ./cmd/queryshift
./bin/queryshift
```

The conversation works immediately without a database. Ask general database
questions, discuss SQL, or plan architecture from the home composer. Use
`/connect` only when you need live schema context or query execution; after the
connection is verified, QueryShift returns to the same conversation with that
context attached. Slash commands run the same tools as the agent and return
their results in chat: `/schema users`, `/erd orders`, `/inspect locks`,
`/query SELECT ...`, and `/modernize <SQL>`. Write SQL passed to `/query`
creates an immutable proposal in chat; `/rehearse`, `/approve`, and the exact
`/apply WRITE <contract-id> <confirmation>` command advance its verified run.
Press `F1` from any screen for the keyboard reference, or `ctrl+e`
on the connection screen to load an existing `DATABASE_URL` without displaying
it. When a connected question asks about health or performance, the chat selects
a focused deterministic read-only tool, shows one compact receipt, and gives the
model labeled schema, workload, query, dependency, or history evidence to cite
as `[E1]`, `[E2]`, and so on. `/evidence` opens the complete source payload for
the latest answer. General chat never wakes the database.
All existing scriptable commands remain available, and `queryshift tui` opens
the same full-screen interface explicitly.

Every write preview now produces a versioned [Change Contract](docs/change-contract.md)
that binds the reviewed SQL to the target and structural schema snapshot. The
contract records risk, affected objects, dependencies, recovery requirements,
and verification probes. QueryShift reloads the schema and validates the full
contract immediately before execution, failing closed on SQL edits, target
changes, or schema drift.

For pull requests and CI, generate and verify the same proof without exposing an
apply command:

```sh
queryshift change plan migrations/change.sql -o change.contract.json
queryshift change verify change.contract.json
```

See the [CI change-proof workflow](docs/ci-change-proof.md).

Disposable rehearsal and local enterprise governance are available through
`queryshift change rehearse`, `policy-check`, `approve`, and
`approval-verify`. See [governance and signed approvals](docs/governance.md).
Neon users can create, verify, and automatically remove an isolated rehearsal
branch with `queryshift change rehearse-neon --project-id …`; the branch
connection URI remains memory-only.

The integrated workflow uses a Verified Change Run. It enforces rehearsal for
elevated-or-higher changes and an independent role-bound approval for
high/critical changes before a production write connection is opened:

```sh
queryshift change run-init change.contract.json -o change.run.json
queryshift change bundle change.run.json -o .queryshift/changes/my-change
queryshift change bundle-verify .queryshift/changes/my-change
```

Compare environments without reading table values, and keep credentials only
in runner-local secret references:

```sh
queryshift environments diff --from-url "$STAGING_DATABASE_URL" --to-url "$PRODUCTION_DATABASE_URL"
queryshift environments config-check .queryshift/environments.json
```

For team execution, `queryshift runner request`, `verify`, and `execute` use
short-lived signed requests. Requests and bundles contain no DSN; the customer
runner resolves `env://VARIABLE_NAME` locally, enforces RBAC and policy, rejects
replayed nonces, and can export a hash-chained JSONL audit trail. See the
[customer-controlled runner guide](docs/enterprise-runner.md).

Modernize a legacy query using a supplied PostgreSQL schema snapshot:

```sh
uv run --project agent queryshift-modernize legacy.sql \
  --source oracle \
  --schema-file target-schema.sql \
  --output modernization-report.json
```

Or add `--live-schema` with a read-only `DATABASE_URL` to ground entity mapping
and safe `EXPLAIN` validation in a PostgreSQL database hosted anywhere.

The inherited diagnostics engine is also available as the `queryshift` binary:

![queryshift inspect — a read-only vital-signs read: headline gauges with a status, then the checks that came back clean](docs/img/dashboard.png)

```sh
curl -fsSL https://raw.githubusercontent.com/sarankumar1325/queryshift/main/install.sh | sh
queryshift inspect "postgres://queryshift_ro@host:5432/db"
```

Or set the connection once in the environment and drop the argument — handy for
CI and shells, and it keeps the password out of your history and `ps`:

```sh
export DATABASE_URL="postgres://queryshift_ro@host:5432/db"
queryshift inspect
```

queryshift reads the argument first, then `$DATABASE_URL`, then `$QUERYSHIFT_DATABASE_URL`,
then `$PGSERVICE` (if you keep your connections in a
[connection service file](https://www.postgresql.org/docs/current/libpq-pgservice.html),
just `export PGSERVICE=mydb` and drop the argument too).
(Shell note: `export DATABASE_URL="…"` — no `$` on the left, no spaces around `=`.)

Everything queryshift takes from the environment fits in one block — the connection,
and (only if you want the optional `ask`/`explain` AI layer) one model key:

```sh
export DATABASE_URL="postgres://queryshift_ro:…@host:5432/db?sslmode=require"

# optional, for `queryshift ask` / `queryshift explain` — one of:
export OPENAI_API_KEY=sk-…        # → OpenAI (gpt-5.6-terra by default)
export GEMINI_API_KEY=…           # → Google Gemini (AI Studio key)
export ANTHROPIC_API_KEY=…        # → Anthropic (claude-opus-5 by default)
export XAI_API_KEY=…              # → xAI (grok-4.6 by default)
# …or any OpenAI-compatible endpoint, local ones included — see "explain — optional AI layer"
```

Everything else — `inspect`, `queries`, `indexes`, MCP, CI — is fully
deterministic and needs **no key**; nothing leaves your machine. Provider
pinning and model/endpoint overrides: [the AI layer](#explain--optional-ai-layer).

```
connected · db.example.com · postgres 17.4 · read-only · 6h20m window

  cache hit  [████████████████████]  99.4%     ok
  lock wait  [████████████░░░░░░░░]  61.0%     query 4f2a
  rollbacks  [██░░░░░░░░░░░░░░░░░░]  12.0%     watch
  idle idx   [██████░░░░░░░░░░░░░░]  18.0 GiB  review

checked · queries · vacuum · replication · checkpoints
        · connections · settings · deadlocks

Database health: 82/100

CRITICAL
● transaction-id age 1.8B — 84% toward wraparound

WARNING
● orders queries 3.2× slower (8 → 26 ms mean)
● 3 unused indexes consume 18 GB
● connection usage reached 87%

Details: queryshift inspect --full   ·   Machine-readable: --json
Ask it: queryshift ask "why is it slow?"
```

The default report is a **graded read**: a four-row gauge strip of vital signs
(cache hit, lock wait with the culprit query, rollbacks, idle index bytes as a
share of the database), a `checked` line naming the subsystems that came back
clean (a tool that names what it verified reads like a colleague who looked,
not an alarm), a health score, then findings bucketed CRITICAL / WARNING /
NOTE. `queryshift inspect --full` adds a subsystem status board plus
the section tables and per-finding caveats; focused commands (`indexes`,
`queries`, `tables`, `vacuum`) each drill into one signal; `queryshift ask "…"` and
`queryshift explain` put a plain-language AI reading on top of the same findings.
`--json` is the complete, versioned contract for agents and scripts.

```
$ queryshift ask "what's wrong?"

Your database is mostly healthy.

1 critical issue:
orders queries became 3.2× slower in the last 6 hours.

Likely cause:
sequential scans increased after the orders table grew 18%.

Recommended:
review an index on customer_id + created_at.
```

## Why queryshift

| | |
|---|---|
| **Read-only by role, not by flag** | The guarantee is a `pg_monitor` login role with no write grants. Session pinning (`default_transaction_read_only`, `statement_timeout=15s`, `lock_timeout=2s`) and `BEGIN READ ONLY` are defence in depth on top of it. |
| **It remembers** | Every run writes a local baseline, so from the third run on it tells you *what changed and why it matters* — a query that got slower, a table that started sequential-scanning, an index that stopped being used. |
| **Findings are deterministic** | Every finding is computed in Go from SQL. The optional AI layer explains findings; it never generates them. |
| **Nothing to deploy** | One static binary. No collector, no time-series database, no service to run. |
| **Built for agents** | `--json` is a versioned, PII-free contract; `queryshift mcp` exposes the same findings over the Model Context Protocol, with a skill and a Claude Code plugin on top. |

<details>
<summary><strong>How it compares to pganalyze, PMM, pgwatch</strong></summary>

queryshift is a **point-in-time diagnostic you run**, not a monitoring platform you
operate. If you want dashboards, alerting, long retention, and multi-host
rollups, run pganalyze / Percona PMM / pgwatch — queryshift doesn't replace them.
Reach for queryshift when you want an answer in ten seconds without deploying
anything, when you're triaging a database you don't own, or when an AI agent
needs structured Postgres findings it can reason over.

</details>

## Requirements

- PostgreSQL 14–18 (16–18 fully supported, 14–15 best-effort); collectors
  degrade rather than fail on older feature sets — see
  [Version support](#version-support).
- A login role holding `pg_monitor` — see
  [Setup](#setup--a-read-only-role-with-pg_monitor).
- `pg_stat_statements` for the queries section (optional; queryshift prints the
  provider-specific install steps when it's missing).
- `advise` additionally needs the [hypopg](https://github.com/HypoPG/hypopg)
  extension and PostgreSQL 16+.
- Linux, macOS, Windows — amd64 and arm64.

## See it

**`queryshift inspect --full`** — a subsystem status board (one row per subsystem,
colored ok / warn / fail), followed by the detailed section tables.

![queryshift inspect --full — a box-drawing subsystem status board](docs/img/full.png)

**`queryshift indexes`** — zero-scan indexes with sizes, and the caveat that matters:
on a primary those scan counts are per-node, so a replica may still be using an
index that looks unused here. It tells you what *not* to drop.

![queryshift indexes — zero-scan indexes and what not to drop](docs/img/indexes.png)

**`queryshift queries`** — the top statements from `pg_stat_statements`, ranked by
total execution time (the query quietly eating your database) with a `share`
column for each query's slice of total time. Add `--by-calls` to rank by call
count instead — a cheap query run a million times can outweigh an expensive one
run twice. Transaction-control and session-`SET` noise is filtered out.
An enabled extension whose statistics could not be read is reported as unavailable,
not as an empty workload. The MCP `top_queries` response preserves `enabled` and
adds `available` to distinguish those states; a successful empty workload has
`enabled: true`, `available: true`, and an empty `queries` array.

```
$ queryshift queries "$DATABASE_URL"
  total  share  calls  mean       query
  4h11m  61.0%  812.4k 18.55 ms   SELECT * FROM orders WHERE user_id = $1 AND …
  22m3s  17.8%  1.3k   1.02 s     SELECT count(*) FROM events WHERE created_at …
  15m2s  12.0%  99.8k  9.04 ms    INSERT INTO audit_log (actor, action, …) VAL …
```

**`queryshift vacuum`** — autovacuum health per table: dead tuples, dead-tuple ratio,
when autovacuum last ran, and a computed `due?` — whether the table's dead tuples
have passed Postgres' default autovacuum trigger (`50 + 20%` of live rows). Rising
dead tuples with `due? yes` and no recent run is autovacuum falling behind, the
early signal for bloat and, eventually, wraparound risk.

```
$ queryshift vacuum "$DATABASE_URL"
  table               live   dead   dead%  last autovacuum  due?
  public.demo_events  42.9k  33.8k  44.1%  4m ago           yes
  public.churny       5.0k   10.0k  66.7%  never            yes
```

**`queryshift tables`** — the largest tables by total size (heap + indexes + TOAST),
each with row count, dead-tuple ratio, and sequential-vs-index scan counts. It's
storage accounting *and* a missing-index radar: a large table with heavy `seq
scans` and few `idx scans` is a likely index candidate.

```
$ queryshift tables "$DATABASE_URL"
  size      rows   dead%  seq scans  idx scans  table
  38.7 GiB  19.7M  8.3%   1.5k       112.3M     public.performance_events
  20.0 GiB  1.3M   10.8%  2.5M       121.6M     public.events        ← 2.5M seq scans
  7.1 GiB   5.6M   0.0%   5.0k       46.6M      public.log_entries
```

**`queryshift ask "why is it slow?"`** — a plain-language reading of the *same*
deterministic findings. It leads with the lock contention and refuses to
recommend dropping the indexes because replication is active — the caveat is
carried into the advice, not lost.

![queryshift ask — an AI reading of queryshift's findings, with caveats carried](docs/img/ask.png)

## Commands and flags

Every command takes the connection the same way — an argument, `$DATABASE_URL`,
`$QUERYSHIFT_DATABASE_URL`, or `$PGSERVICE`.

| Command | What it does |
|---|---|
| `inspect` | the full findings-first health report (`--full` for the section tables) |
| `lint` | schema-only check, safe on an empty CI database (`inspect --profile=schema --no-store`) |
| `init` | generate the read-only role setup SQL — nothing is executed (`--verify` checks an existing role) |
| `diff` | compare two baseline snapshots offline |
| `why` | explain a regression from baseline history: symptom ← mechanism ← antecedent, with numbers and onset times (offline; `--duration 10s` adds a live wait diagnosis) |
| `indexes` · `queries` · `tables` · `vacuum` | drill into one signal |
| `logs` | the server log over SQL — newest entries or `--live` follow (experimental) |
| `waits` | sample where database time goes — wait classes, blockers, contention (experimental) |
| `erd` | the schema as an ER diagram, drawn in the terminal (`--mermaid` for GitHub) |
| `activity` | live sessions right now — PIDs, ages, waits, and what each is running |
| `report` | the full inspection as one self-contained HTML page (`queryshift report > report.html`) |
| `advise` | planner-validated missing-index suggestions (needs hypopg) |
| `ask "…"` · `explain` | a plain-language AI reading of the same deterministic findings |
| `explain-finding <id>` | the catalogue page for a finding, offline |
| `mcp` | serve the findings to an AI agent over MCP |
| `config` · `baselines` | manage `.queryshift.toml` and the local baseline store |

Key `inspect` flags:

| Flag | |
|---|---|
| `--json` · `--format=text\|json\|sarif\|junit\|prometheus` | output format; SARIF uploads to the GitHub Security tab |
| `--fail-on=critical\|warn\|info\|none` | the severity that makes the exit code non-zero (the CI gate) |
| `--profile=full\|schema` | `schema` runs only catalog-derived findings — safe on an empty CI database |
| `--fail-on-new <base.json>` | act only on findings not already in a base report (migration PRs) |
| `--all-databases` | inspect every database in the cluster; cluster-wide findings reported once |
| `--all-instances` | Aurora (experimental): discover every writer and reader instance behind the cluster endpoint and inspect each; composes with `--all-databases` |
| `--config <path>` | a `.queryshift.toml` for thresholds, severity remaps, and `[[ignore]]` rules |

Exit codes are a scriptable contract: `0` clean · `1` warn · `2` critical · `3`
connection/execution failure · `64` usage error. Suppressed and pre-existing
findings never move them.

## Install

| Method | Command |
|---|---|
| npx / bunx / pnpm dlx (no install) | `npx @queryshift/cli inspect "$DATABASE_URL"` |
| Script (cosign signature + checksum) | `curl -fsSL https://raw.githubusercontent.com/sarankumar1325/queryshift/main/install.sh \| sh` |
| Homebrew | `brew install sarankumar1325/tap/queryshift` |
| Arch User Repository | `yay -S queryshift-bin` |
| Go | `go install github.com/sarankumar1325/queryshift/cmd/queryshift@latest` |
| Docker | `docker run --rm ghcr.io/sarankumar1325/queryshift inspect "$DATABASE_URL"` |
| Windows / manual | download the archive for your OS/arch from [Releases](https://github.com/sarankumar1325/queryshift/releases) (Linux/macOS `.tar.gz`, Windows `.zip`) |

`npx @queryshift/cli` fetches the prebuilt binary for your platform from npm (shipped as
an `optionalDependency`, so only the matching one installs) and runs it — nothing to
install, works with `npm ci --ignore-scripts`. It installs the `queryshift` command
(`npm i -g @queryshift/cli`). The package is **scoped**: the bare name `queryshift` is
blocked by npm's package-name-similarity rule (too close to `got`), so
`npx queryshift` returns `E404` — use `npx @queryshift/cli`.

**Any Node package manager works.** npm, pnpm, Yarn (Classic and Berry) and bun
all install it, and each downloads only your platform's binary — the other five
are skipped by the packages' `os`/`cpu` fields:

| | no install | project | global |
|---|---|---|---|
| npm | `npx @queryshift/cli` | `npm i @queryshift/cli` | `npm i -g @queryshift/cli` |
| pnpm | `pnpm dlx @queryshift/cli` | `pnpm add @queryshift/cli` | `pnpm add -g @queryshift/cli` |
| Yarn | `yarn dlx @queryshift/cli` | `yarn add @queryshift/cli` | `yarn global add @queryshift/cli` |
| bun | `bunx @queryshift/cli` | `bun add @queryshift/cli` | `bun add -g @queryshift/cli` |

The wrapper locates the binary at run time and queryshift ships **no install scripts**,
so it works wherever lifecycle scripts are disabled — `npm ci --ignore-scripts`,
pnpm's blocked-by-default builds, bun. Two Yarn notes: `yarn dlx` is Berry-only
(Classic uses `yarn global add`), and under Berry's default Plug'n'Play linker
there is no `node_modules/.bin`, so invoke it as `yarn queryshift`.

Homebrew installs from the [`sarankumar1325/homebrew-tap`](https://github.com/sarankumar1325/homebrew-tap)
tap; the formula is regenerated by every release and pins the SHA-256 of each
platform's release archive. macOS (Intel/Apple Silicon) and Linux (x86_64/arm64).

Against a **remote/managed database** (RDS, Neon, Supabase, …) the container needs
no special networking — it reaches the host directly. Pass the DSN by
**environment**, not as an argument, so the password stays out of `ps` and your
shell history:

```bash
export DATABASE_URL="postgres://queryshift_ro:…@yourdb.example.com:5432/db?sslmode=require"
docker run --rm -e DATABASE_URL ghcr.io/sarankumar1325/queryshift inspect
```

Use a `pg_monitor` role, not a superuser. (For a database in a local container,
see [Postgres in Docker](#postgres-in-docker) — the networking differs.)

**What each path verifies.** npm is the *convenient* path: the packages carry
registry integrity hashes and npm **provenance**, a verifiable link to the GitHub
Actions workflow that built them — that attests *where* the package came from, not
that the artifact was signed. `install.sh` is the *verified* path: releases ship
SHA256 checksums signed with **cosign** (keyless, via GitHub Actions OIDC), and the
script verifies that signature when `cosign` is on your `PATH` and always verifies
the checksum. For the strongest guarantee, require the signature:

```bash
QUERYSHIFT_REQUIRE_SIGNATURE=1 curl -fsSL https://raw.githubusercontent.com/sarankumar1325/queryshift/main/install.sh | sh
```

`QUERYSHIFT_REQUIRE_SIGNATURE=1` hard-fails if `cosign` is missing or the check doesn't
pass. To verify a release by hand:

```bash
cosign verify-blob --bundle checksums.txt.cosign.bundle \
  --certificate-identity-regexp '^https://github.com/sarankumar1325/queryshift/\.github/workflows/release\.yml@' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
```

<details>
<summary><strong>Uninstalling</strong></summary>

```bash
rm "$(command -v queryshift)"
rm -rf "${XDG_STATE_HOME:-$HOME/.local/state}/queryshift"   # local baseline store
```

queryshift writes nothing else on your machine — no daemons, no launch agents — and
nothing in the database: no extensions, no tables, no roles.

</details>

## Setup — a read-only role with `pg_monitor`

The read-only guarantee is **the role**, not a flag. Create a login role that
holds `pg_monitor` (so it can see the full statistics views) and has no write
grants:

```sql
CREATE ROLE queryshift_ro LOGIN PASSWORD '...';
GRANT pg_monitor TO queryshift_ro;
GRANT CONNECT ON DATABASE yourdb TO queryshift_ro;
```

Or have `queryshift init` write exactly this for you — tailored to your provider and
database name, including the provider-specific `pg_stat_statements` step.
queryshift itself executes none of it (queryshift never writes); you review and run it:

```sh
queryshift init "postgres://admin@host:5432/db" | psql "postgres://admin@host:5432/db"
queryshift init --verify "postgres://queryshift_ro:…@host:5432/db"   # confirm the role works
```

Without `pg_monitor`, a non-superuser sees only its own sessions in
`pg_stat_activity` and can't read several views fully — queryshift detects this at
connect time and tells you exactly which GRANT to run rather than silently
reporting partial data.

queryshift additionally pins every session read-only (`default_transaction_read_only`,
`statement_timeout=15s`, `lock_timeout=2s`) and wraps each query in its own
`BEGIN READ ONLY … COMMIT`. It **commits** those read-only probes rather than
rolling them back — a read-only transaction writes nothing either way, but a
rollback would inflate the `xact_rollback` counter queryshift itself reports. Those
are defence in depth; the role is the boundary.

### What it costs your database

A run opens **one connection pool capped at 4 connections**, holds no long
transactions (every probe is its own `BEGIN READ ONLY … COMMIT` under the
pinned `statement_timeout=15s` / `lock_timeout=2s`), and takes no locks beyond
the shared catalog access any `SELECT` takes. Counters are sampled twice across
the `--interval` gap (default 1s), so a full `inspect` finishes in a few
seconds of wall clock. It is safe to run against a busy primary — queryshift even
excludes its own sessions, transactions, and temp usage from what it reports,
so it never measures its own footprint as the database's. Run it against a
replica if you prefer, noting the per-node index-scan caveat in
[`queryshift indexes`](#see-it).

## Point queryshift at your database

Pass the connection string as an argument — a URL or a libpq DSN:

```bash
queryshift inspect "postgres://queryshift_ro:secret@host:5432/db?sslmode=require"

# the libpq keyword/value DSN form works too:
queryshift inspect "host=host port=5432 dbname=db user=queryshift_ro sslmode=require"
```

Or set it once in the environment and omit the argument — convenient for a shell
session or CI, and it keeps the password out of your shell history and `ps`
output:

```bash
export DATABASE_URL="postgres://queryshift_ro:secret@host:5432/db?sslmode=require"

queryshift inspect
queryshift queries     # every command takes the connection the same way
queryshift diff --since 24h
```

queryshift resolves the connection in this order: the argument first, then
`$DATABASE_URL`, then `$QUERYSHIFT_DATABASE_URL`. Add `?sslmode=require` (or stricter)
for any database reached over a network.

### Reaching a private database

A database on a private network — RDS/Aurora inside a VPC, or a Postgres behind a
bastion — is reached through an SSH jump host:

```sh
queryshift inspect "postgres://queryshift_ro@db.internal:5432/appdb?sslmode=verify-full" \
  --ssh-tunnel bastion.example.com    # or user@host:port, or a ~/.ssh/config alias
```

`--ssh-tunnel` is global — every command that opens a connection takes it — and
`$QUERYSHIFT_SSH_TUNNEL` sets it for a whole session.

The tunnel is a dialer, not an `ssh -L` forward, so **the DSN keeps naming the real
host**: `sslmode=verify-full` still validates against that hostname, `.pgpass` still
matches on it, and no local port is left open to everyone else on your machine.

How the jump host is reached comes from your own `ssh_config` — `HostName`, `Port`,
`User`, `IdentityFile`, `IdentitiesOnly`, `IdentityAgent`, `StrictHostKeyChecking`,
`UserKnownHostsFile` — so a bare alias works and the host key is verified exactly
the way your `ssh` verifies it: a host seen for the first time is accepted under
your `StrictHostKeyChecking` setting and recorded in your known_hosts, and a key
that later changes is refused. Your agent is offered before any key on disk, and
one SSH connection serves the whole run. Raise `--timeout` if the link is slow.

### Environment reference

| Variable | Purpose |
|---|---|
| `DATABASE_URL` / `QUERYSHIFT_DATABASE_URL` / `PGSERVICE` | Connection used when no connection string is passed (checked in that order, after the argument). `PGSERVICE` picks a `[section]` from your [connection service file](https://www.postgresql.org/docs/current/libpq-pgservice.html) (`PGSERVICEFILE`, or the libpq default path). |
| `NO_COLOR` | Disables ANSI output (as does a non-TTY, or `--no-color`). |
| `XDG_STATE_HOME` | Where the baseline store lives; defaults to `~/.local/state`. |
| `QUERYSHIFT_SSH_TUNNEL` | SSH jump host used when `--ssh-tunnel` isn't passed (`[user@]host[:port]`, or a `~/.ssh/config` alias). |
| `QUERYSHIFT_CONFIG` | Path to `.queryshift.toml` (otherwise discovered from cwd upward, then `$XDG_CONFIG_HOME`). |
| `OPENAI_API_KEY` / `OPENROUTER_API_KEY` | Enables `ask` / `explain` via OpenAI or OpenRouter. Keys are never accepted as flags. |
| `REQUESTY_API_KEY` | Enables `ask` / `explain` via [Requesty](https://docs.requesty.ai) with `QUERYSHIFT_AI_PROVIDER=requesty` (never auto-detected). |
| `OPPER_API_KEY` | Enables `ask` / `explain` via [Opper](https://docs.opper.ai) with `QUERYSHIFT_AI_PROVIDER=opper` (never auto-detected). |
| `GEMINI_API_KEY` / `GOOGLE_API_KEY` | Enables `ask` / `explain` via Google Gemini. |
| `ANTHROPIC_API_KEY` | Enables `ask` / `explain` via Anthropic. |
| `XAI_API_KEY` / `GROK_API_KEY` | Enables `ask` / `explain` via xAI. |
| `AWS_BEARER_TOKEN_BEDROCK`, or `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN` | Enables `ask` / `explain` via AWS Bedrock with `QUERYSHIFT_AI_PROVIDER=bedrock` (never auto-detected). `AWS_REGION` picks the endpoint; `AWS_CREDENTIAL_EXPIRATION` bounds the minted token. |
| `QUERYSHIFT_AI_PROVIDER` | `gemini`, `anthropic`, `openai`, `xai`, or `bedrock` (alias `mantle`) — picks one when several keys are set (auto-detection tries OpenAI first). |
| `QUERYSHIFT_AI_MODEL` / `QUERYSHIFT_AI_BASE_URL` / `QUERYSHIFT_AI_API_KEY` | Model, endpoint, and key override for whichever provider is selected; the way to reach an OpenAI-compatible service (OpenRouter, Groq, Ollama, vLLM, …). |
| `QUERYSHIFT_AI_REASONING_EFFORT` | `none`, `low`, `medium`, `high`, `xhigh`, or `max` for reasoning models (OpenAI's default here is `xhigh`). |
| `QUERYSHIFT_OPENAI_MODEL` / `QUERYSHIFT_OPENAI_URL` | Still honored: OpenAI-scoped model/endpoint override. |
| `QUERYSHIFT_GEMINI_MODEL` / `QUERYSHIFT_GEMINI_URL` | Still honored: Gemini-scoped model/endpoint override. |
| `QUERYSHIFT_REQUIRE_SIGNATURE` | `install.sh` only: hard-fail unless the cosign signature verifies. |

## Connecting to managed providers

queryshift is a **client** — it connects over the Postgres wire protocol like `psql`.
You never install anything on the database; run queryshift from your laptop, a bastion,
CI, or an instance in the same network. Grant `pg_monitor` to your role (above)
and connect. Provider-specific notes:

### AWS RDS / Aurora

You can't install on the RDS/Aurora instance itself — it's managed, no OS access.
Run queryshift from a **client that can reach it**:

- **Private RDS (recommended for prod):** run queryshift from a small **EC2 in the same
  VPC**. It reaches the private endpoint over AWS's internal network — no public
  access, no SSH tunnel, no IP allow-listing. The only rule is the RDS security
  group allowing `5432` from the EC2's security group.
- **Publicly accessible RDS:** allow your IP in the RDS security group and connect
  straight from your laptop.

```bash
# on the EC2 (or your laptop for a public instance):
curl -fsSL https://raw.githubusercontent.com/sarankumar1325/queryshift/main/install.sh | sh
queryshift inspect "postgres://queryshift_ro@mydb.abc123.us-east-1.rds.amazonaws.com:5432/appdb?sslmode=require"
```
Grant `pg_monitor` as the master (`rds_superuser`) role. **Caveat:** host metrics
(CPU / memory / disk IOPS) live in CloudWatch, not Postgres, so they're out of
reach over a connection string — everything else works.

### Neon

```bash
queryshift inspect "postgres://user:pass@ep-xxx.region.aws.neon.tech/dbname?sslmode=require"
```
- The **pooled** endpoint has a `-pooler` host suffix (transaction mode). queryshift
  detects it and proceeds — rates stay correct — or use the direct (non-pooler)
  host for session-scoped certainty.
- Neon's default string ships `channel_binding=require`; queryshift **ignores it
  automatically** (the driver can't do channel binding; TLS from `sslmode` still
  applies) instead of erroring.
- `pg_stat_statements` is preloaded — just `CREATE EXTENSION pg_stat_statements;`.
- **Scale-to-zero:** after idle, Neon suspends the compute and discards stats. The
  first run after a wake is a *cold window* — queryshift suppresses counter-based
  findings until the window is old enough, so a reset never reads as a −99% regression.

### Supabase

```bash
# direct endpoint (session-scoped, best for queryshift):
queryshift inspect "postgres://postgres:pass@db.<ref>.supabase.co:5432/postgres?sslmode=require"
# or the pooled endpoint (:6543, transaction mode) — queryshift notes it and proceeds:
queryshift inspect "postgres://postgres.<ref>:pass@aws-0-<region>.pooler.supabase.com:6543/postgres?sslmode=require"
```
- The default pooled connection string uses port **`:6543`** (Supavisor, transaction
  mode). queryshift detects the pooler and proceeds with a note; prefer the direct
  `:5432` endpoint when you can.
- `pg_stat_statements` is preloaded — `CREATE EXTENSION pg_stat_statements;`.
- Supabase doesn't hand out superuser; the built-in `postgres` role already has
  broad read access, or grant `pg_monitor` to a dedicated role where allowed.

### Postgres in Docker

The connection string depends on **where queryshift runs relative to the container.**

**queryshift on the host, container with a published port.** Read the `PORTS` column of
`docker ps` — `0.0.0.0:6433->5432/tcp` means host port `6433` maps to the
container's `5432`. Connect to the **host** port:

```bash
docker port mypg 5432                    # → 0.0.0.0:6433  (find the host port)
queryshift inspect "postgres://postgres:pw@127.0.0.1:6433/postgres?sslmode=disable"
```

Use `127.0.0.1`, not `localhost`: `localhost` resolves to IPv6 (`::1`) first, which
Docker Desktop doesn't forward, so the connect stalls ~10s before falling back to
IPv4. Local containers usually have no TLS → `sslmode=disable`. Find the
credentials with `docker exec mypg env | grep POSTGRES`.

**queryshift as a container reaching a DB container.** `localhost` would mean queryshift's
own container — join the DB's network and use the **container name** + internal
port `5432`:

```bash
docker run --rm --network <that-network> ghcr.io/sarankumar1325/queryshift \
  inspect "postgres://postgres:pw@mypg:5432/postgres?sslmode=disable"
```

The image is multi-arch (amd64/arm64) and public — no login needed. Prefer passing
the DSN by environment so it stays out of the container's argument list:

```bash
docker run --rm --network <that-network> -e DATABASE_URL ghcr.io/sarankumar1325/queryshift inspect
```

**queryshift as a container reaching a DB on the host.** Use `host.docker.internal`
(add `--add-host=host.docker.internal:host-gateway` on Linux).

> Rule of thumb: same-network containers address each other by **container name +
> internal port `5432`**; the host reaches a container by **`127.0.0.1` + the
> published host port**. A container with no `->` mapping in `docker ps` isn't
> reachable from the host at all — publish it with `-p`, or connect from inside
> its network.

## Usage

```
queryshift inspect <connection-string>   # URL or libpq DSN, or set $DATABASE_URL
  --json                 emit the versioned, PII-free Context (the agent/script contract)
  --interval 1s          gap between the two counter samples (min 500ms)
  --no-store             don't read or write the local baseline
  --no-color             disable ANSI (also honors NO_COLOR and non-TTY)
  --ssh-tunnel <host>    reach the database through an SSH jump host — global, so
                         every command that connects takes it (also $QUERYSHIFT_SSH_TUNNEL)

queryshift baselines list                # what's stored locally, per database
queryshift baselines prune <fingerprint> # delete a database's snapshots
queryshift baselines export <fingerprint># dump stored snapshots as JSON

queryshift indexes <connection-string>   # zero-scan indexes + what NOT to drop
  --correlate            grade each index (catalog_proven/needs_code_check/inconclusive) + what to grep in code
queryshift queries <connection-string>   # top pg_stat_statements by total time (--by-calls to re-rank)
queryshift tables  <connection-string>   # largest tables + row counts + seq-vs-index scan pattern
queryshift vacuum <connection-string>    # autovacuum health per table — dead tuples + whether it's due
queryshift tune <connection-string>      # config-tuning recommendations from the workload
queryshift explain <connection-string>   # inspect, then have an AI explain the findings
queryshift ask "why is it slow?"         # AI answer grounded on the findings ($DATABASE_URL)
  --yes                  skip the data-disclosure confirmation prompt
queryshift mcp                           # run as an MCP server over stdio (for AI agents)
```

### MCP — use queryshift as an agent tool

`queryshift mcp` speaks the [Model Context Protocol](https://modelcontextprotocol.io)
on stdio, so an AI agent can call queryshift as a read-only tool. It exposes
**deterministic** tools only and lets the *connected model* do the explaining:

- `inspect` — full findings as JSON
- `unused_indexes`, `top_queries`, `vacuum_health` — the CLI's focused views
- `suggest_indexes` — planner-validated index recommendations (hypopg)
- `index_code_correlation` — grades each unused/redundant/invalid index
  (`catalog_proven` / `needs_code_check` / `inconclusive`) and, for the actionable
  ones, the exact identifiers to grep (all case conventions) with the instruction
  to search *filters* (WHERE/JOIN/ORDER BY), never SELECT lists. queryshift never reads
  your repo; it says what to search for. Also `queryshift indexes --correlate [--json]`.
- `record_index_verdict` — store what the repo search found, so a later run over a
  longer window carries strengthening evidence (no DB connection needed)
- `explain_plan` — the planner's plan for a SELECT (plain EXPLAIN, never executed)
- `schema_catalog` — bounded user-schema snapshot for semantic mapping, **no data**
- `schema_of` — a table's columns/indexes/constraints + row estimate, **no data**
- `compare_to_baseline` — the `diff`, with its interval-honesty and reset caveats
- `why` — the causal chains from stored history (symptom ← mechanism ←
  antecedent), computed offline from the local store; at least four stored snapshots
  are needed to produce the three interval observations required for a sustained change
- `explain_finding` — queryshift's catalogue page for a finding, so the agent explains
  a recommendation in queryshift's words instead of inventing them

Every tool is read-only, returns a stable JSON shape carrying its `exactness`
label, honors `.queryshift.toml` suppression, and never exposes a raw connection
string or query literals to the model. The agent reasons over the same findings
the CLI computes.

In Claude Code it's one line:

```sh
claude mcp add queryshift --env DATABASE_URL="postgres://queryshift_ro:…@host:5432/db?sslmode=require" -- queryshift mcp
```

Or add it to any MCP client's config (Claude Desktop, Cursor, …):

```json
{
  "mcpServers": {
    "queryshift": {
      "command": "queryshift",
      "args": ["mcp"],
      "env": { "DATABASE_URL": "postgres://queryshift_ro@host:5432/db" }
    }
  }
}
```

With `DATABASE_URL` set, the agent calls `inspect` with no arguments; or it can
pass `connection_string` per call to reach several databases. queryshift never writes,
so there's nothing an agent can break through it.

It also exposes a **`diagnose` prompt** (a one-click "inspect and give me a
prioritized diagnosis" workflow) and a **`queryshift://baselines` resource** (the
databases queryshift has local history for) — so tools, prompts, and resources are all
available to the agent.

**Pair it with the skill.** MCP gives the agent the *tools*; the
[`postgres-diagnostics` skill](skills/postgres-diagnostics/SKILL.md) gives it the
*playbook* — respect caveats, never `EXPLAIN ANALYZE`, prioritize by impact,
never write. One command installs it into Claude Code, Cursor, or Codex:

```bash
npx skills add sarankumar1325/queryshift
```

(or `curl -fsSL https://raw.githubusercontent.com/sarankumar1325/queryshift/main/skills/install.sh | sh` — see [`skills/`](skills/)), and
your agent asks the right queryshift command and reads the results the way queryshift
intends.

### Claude Code plugin

[Claude Code](https://claude.com/claude-code) users can install the tools, the
skill, and the commands in one shot — the repo is its own plugin marketplace:

```bash
claude plugin marketplace add sarankumar1325/queryshift
claude plugin install queryshift@queryshift
```

That registers the queryshift **MCP tools**, the **`postgres-diagnostics` skill**, and
three slash commands — **`/pg-health`**, **`/pg-slow`**, **`/pg-indexes`** — each
of which carries the queryshift judgment (caveats intact, impact-first, never writes).
The plugin drives the `queryshift` binary, so install that first (`curl -fsSL
https://raw.githubusercontent.com/sarankumar1325/queryshift/main/install.sh | sh`); set `DATABASE_URL` or pass a connection string
per call, then ask *"is my Postgres healthy?"*

### Strands agent — Bedrock model, any PostgreSQL host

[`agent/`](agent/) is an optional open-source agent host built with the Strands
Agents SDK. Amazon Bedrock supplies the model; queryshift's existing MCP server
supplies the deterministic, read-only tools. The database itself does **not**
need to be on AWS — the same agent works with RDS/Aurora, Neon, Supabase, Cloud
SQL, Azure Database for PostgreSQL, or self-hosted PostgreSQL.

```bash
export DATABASE_URL='postgres://queryshift_ro:…@host:5432/db?sslmode=require'
export AWS_REGION=us-east-1
export QUERYSHIFT_AGENT_MODEL='deepseek.v3.2'

uv run --project agent queryshift-agent 'Why is this database slow?'
```

For local testing, copy `.env.example` to the git-ignored `.env` in the
repository root and fill in your AWS credentials. The Python agent commands load
it automatically without replacing variables already exported by your shell.

The loop is bounded by turn and token budgets. It exposes only an allowlist of
read-only MCP tools; the target connection is injected through the trusted
subprocess environment and the model cannot supply or see a connection-string
argument. See the [agent README](agent/README.md) for setup and configuration.

#### Enterprise Query Modernizer — five-agent SQL migration

The same package now includes `queryshift-modernize`, a role-separated Strands
pipeline for migrating one legacy read-only query to PostgreSQL:

```text
Schema Mapper → Dialect Transpiler → Validator ↔ Repair → Optimizer → Judge
```

```bash
uv run --project agent queryshift-modernize legacy.sql \
  --source oracle \
  --schema-file target-schema.sql \
  --output modernization-report.json
```

Add `--live-schema` to ground mapping and validation in the PostgreSQL catalog
behind `DATABASE_URL`. That mode exposes only `schema_catalog`, `schema_of`, and
plain `explain_plan`; it never reads application rows, executes the migrated
query, applies an index, or changes the database. Output includes modernized
SQL, proposed indexes, AST/semantic validation, a diff, verification probes,
heuristic confidence/risk, and a per-stage trace. See the
[agent README](agent/README.md#enterprise-query-modernizer) for full usage.

### `explain` — optional AI layer

`queryshift explain` runs the exact same read-only inspection, prints the
deterministic report unchanged, then asks a model to **explain and prioritize**
the findings in plain language. The findings are still computed locally in Go —
the model only interprets them, it never invents them, and it's instructed to
carry every caveat into any recommendation. The AI text is printed below a
labeled rule (`🤖 generated by … — verify before acting`); if the model errors
or the key is unset, the deterministic report still stands.

With a remote model, this sends the same PII-free Context shown by
`inspect --json`. Before sending it, queryshift identifies the provider, host, and
model and asks for confirmation. Local endpoints are identified as local and do
not require confirmation.

| Provider | Key | Default model | API |
|---|---|---|---|
| Gemini | `GEMINI_API_KEY` / `GOOGLE_API_KEY` | `gemini-flash-latest` | `generateContent` |
| Anthropic | `ANTHROPIC_API_KEY` | `claude-opus-5` | `/v1/messages` |
| OpenAI | `OPENAI_API_KEY` | `gpt-5.6-terra` | `/chat/completions` |
| xAI | `XAI_API_KEY` / `GROK_API_KEY` | `grok-4.6` | `/responses` |
| Bedrock Mantle | `AWS_BEARER_TOKEN_BEDROCK`, or `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | `openai.gpt-5.6-terra` | Responses (GPT) / Messages (Claude) |

The OpenAI provider also supports compatible services such as OpenRouter,
Requesty, Opper, Groq, Together, DeepSeek, Mistral, Ollama, vLLM, and LM Studio.
`QUERYSHIFT_AI_PROVIDER=requesty` with `REQUESTY_API_KEY` points it at
`https://router.requesty.ai/v1`; set `QUERYSHIFT_AI_MODEL` to any Requesty model id
(for example `openai/gpt-4o-mini`), and `QUERYSHIFT_AI_BASE_URL` to
`https://router.eu.requesty.ai/v1` to keep requests in the EU.
`QUERYSHIFT_AI_PROVIDER=opper` with `OPPER_API_KEY` points it at
`https://api.opper.ai/v3/compat`, an EU-hosted gateway. The default
`gpt-5.6-terra` works there as is; set `QUERYSHIFT_AI_MODEL` to any Opper model id
(for example `claude-sonnet-4-6`).

```
export OPENAI_API_KEY=…
queryshift explain "$DATABASE_URL"
```

Use `QUERYSHIFT_AI_PROVIDER` to select a provider explicitly. `QUERYSHIFT_AI_MODEL`,
`QUERYSHIFT_AI_BASE_URL`, `QUERYSHIFT_AI_API_KEY`, and `QUERYSHIFT_AI_REASONING_EFFORT`
override its defaults. Existing `QUERYSHIFT_GEMINI_MODEL` and `QUERYSHIFT_GEMINI_URL`
and `QUERYSHIFT_OPENAI_MODEL` and `QUERYSHIFT_OPENAI_URL` settings remain supported. Keys
are read only from environment variables.

For AWS Bedrock, select `bedrock` (or its alias `mantle`) and give queryshift
AWS credentials the same way you give it any other key — through the environment.
No AWS SDK, no config files read, no calls to STS or instance metadata: queryshift
mints Bedrock's bearer token itself from the standard three variables.

```sh
export QUERYSHIFT_AI_PROVIDER=bedrock
export AWS_REGION=us-east-1
# a profile, an SSO login, or an assumed role becomes the three variables:
eval "$(aws configure export-credentials --profile your-profile --format env)"
queryshift ask "What needs attention?" "$DATABASE_URL"

# Claude uses the Anthropic Messages API automatically:
export QUERYSHIFT_AI_MODEL=anthropic.claude-sonnet-5
queryshift ask "What needs attention?" "$DATABASE_URL"
```

Authentication precedence is `QUERYSHIFT_AI_API_KEY`, then `AWS_BEARER_TOKEN_BEDROCK`
(a Bedrock API key from the console), then `AWS_ACCESS_KEY_ID` /
`AWS_SECRET_ACCESS_KEY` (plus `AWS_SESSION_TOKEN` for temporary credentials).
An explicit token is sent as-is. From access keys queryshift mints a fresh bearer
token per request, valid for at most 15 minutes and never past
`AWS_CREDENTIAL_EXPIRATION` when that is set (the export command sets it).
`AWS_PROFILE` on its own does not authenticate — export it as above.

Region precedence is `AWS_REGION`, `AWS_DEFAULT_REGION`, then `us-east-1`. The
default model is `openai.gpt-5.6-terra`. OpenAI GPT models use
`https://bedrock-mantle.<region>.api.aws/openai/v1` and the Responses API, as
documented by
[AWS for OpenAI GPT models](https://aws.amazon.com/blogs/machine-learning/get-started-with-openai-gpt-5-6-sol-terra-and-luna-on-amazon-bedrock/);
models beginning with `anthropic.` use
`https://bedrock-mantle.<region>.api.aws/anthropic` and the Messages API, while
models beginning with `deepseek.` use
`https://bedrock-runtime.<region>.amazonaws.com/openai/v1` and Chat Completions
because DeepSeek V3.2 does not support the Responses API. Set
`QUERYSHIFT_AI_MODEL` to the exact Bedrock model ID available to your account and
region, including for GPT-6 models. `QUERYSHIFT_AI_BASE_URL` overrides the base URL,
without the final `/responses`, `/chat/completions`, or `/v1/messages`. Access
keys are only ever sent to the matching Bedrock HTTPS host for the configured
region, and Bedrock requests never follow redirects.

Responses requests set `store=false`, omit sampling temperature for GPT-5/6
reasoning models, and allow at least 32,000 output tokens (including hidden
reasoning). `QUERYSHIFT_AI_REASONING_EFFORT` is optional for Responses; when unset,
the service chooses its default. Claude keeps the existing Messages request
shape. Neither protocol retries inference automatically.

**Exit codes** (a stable contract for CI): `0` clean · `1` warnings · `2` critical
findings · `3` connection/execution failure · `64` usage error (bad flags/args).
Suppressed findings never contribute to the exit code.

### `advise` — index suggestions the planner validates

`queryshift advise` finds missing indexes without guessing. It reads the slowest
queries from `pg_stat_statements`, derives candidate indexes from the planner's
own sequential-scan filters (deterministically, in Go — never an LLM), and then
**validates each one**: it creates the index *hypothetically* with
[hypopg](https://github.com/HypoPG/hypopg), re-plans the query, and only reports
it if the planner actually switches to it and the estimated cost drops.

```
$ queryshift advise "$DATABASE_URL"
index advisor · app · postgres 17 · hypopg validation — nothing was built

1 validated recommendation(s):

⚑ public.orders
  CREATE INDEX ON public.orders (customer_id, status);
  helps: SELECT count(*) FROM orders WHERE customer_id = $1 AND status = $2
         60 calls · 68% of DB time
  planner confirmed: cost 4653 → 4.1 (−99.9%)
  ↳ nothing was created. Review, then build off-peak: add CONCURRENTLY.
```

Nothing is ever built — the hypothetical indexes live in backend memory and are
discarded. Everything runs in a READ ONLY transaction; queryshift only *plans* your
query (`EXPLAIN (GENERIC_PLAN)`), it never executes it and never uses the
executing form of EXPLAIN. Requires **hypopg**, **pg_stat_statements**, and
**PostgreSQL 16+**; when any is missing it prints exactly what to enable and does
nothing else. `--json` gives structured recommendations for agents (also exposed
as the MCP `suggest_indexes` tool).

> **Local Docker gotcha:** with a database in Docker Desktop, connect via
> `127.0.0.1`, not `localhost`. `localhost` resolves to IPv6 (`::1`) first, which
> Docker Desktop doesn't forward, so the connect stalls for ~10s before falling
> back to IPv4. Managed hosts (RDS, Supabase, Neon…) aren't affected.

The baseline store lives at `$XDG_STATE_HOME/queryshift/baselines.db` (7 days at full
resolution, hourly rollups to 90 days, 100 MB cap). It's yours — inspect and
delete it with `queryshift baselines`.

## What it collects

All from SQL — connections, cache-hit ratio, TPS and rollback ratio, WAL and IO
rates, checkpoints, locks and blocking chains, replication lag, replication-slot
WAL retention and logical-subscription health, top queries
(`pg_stat_statements`), table/index sizes, dead tuples and vacuum activity,
unused and missing indexes, non-default settings, collation version drift
(PG15+), and published tables with no replica identity. Counters
(`pg_stat_database`, `pg_stat_wal`, IO) are **double-sampled** to produce live
rates; the rest are point-in-time reads trended against the baseline.

## The `--json` contract

`--json` (and `--format=json`) is the interface to build on — a versioned,
PII-free document (`schema_version`, currently `1.5.0`) whose machine-checkable
JSON Schema is published in [`schema/`](schema/). Every section carries an
`exactness` label — `sampled`, `cumulative`, `scraped`, or `unavailable` — so a
consumer never mistakes a cumulative total for a live rate.

Versioning policy: additive fields bump the minor version and are not breaking —
a `1.4.0` consumer parses `1.5.0` output unchanged; breaking changes to the
contract are treated as breaking changes to the tool. `queryshift advise --json` has
its own schema
([`schema/queryshift-advise-1.0.0.json`](schema/queryshift-advise-1.0.0.json)).

## Version support

Collectors degrade rather than fail when a capability is absent:

| Feature | From | Fallback |
|---|---|---|
| `pg_stat_wal` (WAL rates) | PG 14 | section marked unavailable |
| `pg_stat_io` (buffers written) | PG 16 | `pg_stat_bgwriter` |
| `pg_stat_checkpointer` | PG 17 | `pg_stat_bgwriter` |
| `stats_fetch_consistency` | PG 15 | separate per-sample transactions |
| `pg_stat_statements` | extension | queries section unavailable + install hint |

### Supported versions

| Tier | Versions | In CI |
|---|---|---|
| **Supported** | PostgreSQL 16, 17, 18 | every PR + push |
| **Best-effort** | PostgreSQL 14, 15 | every PR + push |
| Unsupported | PostgreSQL 13 and older | — (13 is [end-of-life](https://www.postgresql.org/support/versioning/)) |

New features may target 16+ without a backward path. Everything degrades rather
than errors on an older or capability-limited server (see the table above).

### Managed providers

queryshift detects the platform (RDS, Aurora, Cloud SQL, Azure Flexible Server,
Supabase, Neon) and prints the provider-specific steps to enable
`pg_stat_statements` when it's missing. Supabase (`:6543`) and Neon (`-pooler`)
default to a pooled endpoint, which queryshift notes without degrading its rates;
Neon's scale-to-zero discards stats, which queryshift handles as a cold window. Full
per-provider notes and the live-verification checklist are in
[`docs/providers.md`](docs/providers.md).

## Configuration & suppression

An optional `.queryshift.toml` (committed to your repo) overrides thresholds, remaps a
finding's severity, and suppresses specific findings so noise never trains people
to ignore the severity column:

```toml
schema = 1
[severity]
checksums_disabled = "info"        # can't change it on this managed provider
[[ignore]]
finding = "unused_indexes"
object  = "public.idx_legacy_*"    # glob; omit to mute the whole finding
reason  = "backs the quarterly export"
expires = "2026-12-31"
```

Suppression is always **visible** — suppressed findings stay in `--json` (with
`suppressed`/`suppression_reason`), never affect the exit code, and a suppressed
**critical still renders** (a config must not hide `checksum_failures`). queryshift
refuses to read any credential-shaped key from the file, flags rules that have
gone stale, and ships `queryshift config check` / `explain` / `init`. Full contract —
including the per-finding object-identity table — in
[`docs/configuration.md`](docs/configuration.md).

### `diff` — what changed since last time

`queryshift diff [--since 24h]` compares the two most relevant baseline snapshots from
the local store — no connection needed. It's honest about what it compared:

```
$ queryshift diff --since 24h
diff · prod · a1b2c3d4e5f6
2026-08-16 09:00 → 2026-08-17 16:00  ·  31h elapsed
note: you asked for ~24h back, but the nearest older snapshot is 31h back — comparing that.
```

It prints the interval it *actually* used (the nearest snapshot to `--since`, not
a silent substitution), warns up front when a **stats reset** or
**pg_stat_statements eviction** between the snapshots makes specific deltas
untrustworthy, and refuses to compare two different databases (pass
`--fingerprint` when the store holds more than one).

### `why --duration` — live diagnosis: executing, or waiting?

```sh
queryshift why --duration 10s          # sample the live database, then diagnose
```

`why` stays fully offline by default. With `--duration` it also runs a wait
study (the same engine as `queryshift waits`) and classifies the result through a
deterministic, first-match cause table: **transaction lock contention** (only
with a sustained, named blocker), **storage/WAL wait** (IO alone never claims
a missing index), **client/application wait** (explicitly not a PostgreSQL
problem), **saturated with active work**, **not significant** (waits on
near-zero activity are noise), or **insufficient evidence** — refusing to
conclude beats a confident guess. When the local store holds wait history, a
labeled ratio corroborates: *"Lock waits 8× vs the previous 24h."* Shares are
sampled; the only exact numbers are ages read from the server.

### `erd` — the schema, as a diagram, in your terminal

```sh
queryshift erd                # box-drawn tables + a crow's-foot relationship forest
queryshift erd --schema app   # one schema only
queryshift erd --layout row   # left-to-right, dashed ascii edges
queryshift erd --mermaid      # erDiagram text — pasteable into GitHub or mermaid.live
queryshift erd --html > schema.html   # self-contained interactive SVG: pan, zoom, share
```

Structure only — tables, columns, keys, foreign-key edges from `pg_catalog` —
never data, and the connection string never leaves your machine (unlike
paste-your-DSN diagram websites). Needs nothing beyond CONNECT. The `--html`
file is fully self-contained — inline SVG and inline script, zero external
requests — so the schema never leaves the file either.

### `waits` — where database time goes (experimental)

```sh
queryshift waits                         # sample 10s: wait classes, top events, blockers
queryshift waits --duration 30s --group query
queryshift waits --pid 18442             # one backend's story
queryshift waits --json                  # versioned queryshift-waits document (scrubbed)
```

Samples `pg_stat_activity` at 10 Hz and the lock graph (`pg_blocking_pids`)
at 1 Hz for a bounded window — no extension, no eBPF, no daemon, plain
`pg_monitor`. Reports average active sessions, DB time by wait class, top wait
events, waiting sessions, and **blockers with evidence**: a holder is named
only when seen across several lock snapshots (one glimpse is listed as
transient, never blamed). Every share is labeled *sampled* — the only exact
numbers are ages read from the server — and lock contention is explicitly
called out as *not* evidence of a missing index. Aggregated wait counts fold
into the local baseline store (`--no-store` skips), so `diff` and `why` gain
wait history over time.

### `logs` — the server log, over SQL (experimental)

```sh
queryshift logs               # the newest 100 entries, typed: query / info / warn / error
queryshift logs --last 20     # fewer (or more)
queryshift logs --live        # keep following — Ctrl+C to stop (--follow / -f works too)
queryshift logs --level warn,error --json   # scrubbed NDJSON for scripts and agents
```

No agent and no file access: queryshift reads the server's own logfile through
`pg_current_logfile()` and `pg_read_binary_file()`, whichever format it writes
(jsonlog, csvlog, or stderr with any `log_line_prefix`), and follows rotation.
queryshift's own footprint — its probe, its polling reads — is filtered out of the
stream, so a live tail never reads its own reads, and `--last 100` means 100
entries of *your* activity. The human output shows log lines verbatim; `--json`
passes every message through the same literal scrubber as the rest of queryshift.

Reading log content is the one thing `pg_monitor` cannot do, so this command
needs a single extra grant, printed exactly when it's missing:

```sql
GRANT EXECUTE ON FUNCTION pg_read_binary_file(text, bigint, bigint, boolean) TO queryshift_ro;
```

(`queryshift init` includes it commented out; `queryshift init --logs` emits it active.)

A server without a log collector (`logging_collector=off` — the Docker image
default) has no file to read; queryshift says so and points you at `docker logs`.
Managed providers that keep logs behind their own APIs (Neon, Supabase) are out
of reach over SQL — that's their boundary, not a queryshift flag away.

> **Whole cluster:** `queryshift inspect "$DATABASE_URL" --all-databases` inspects every
> connectable database on the server. Cluster-wide findings (settings, replication,
> archiving, wraparound) are reported once; per-database findings appear per
> database. Serial by default (`--parallel N` to fan out).

## CI integration

queryshift is built to run in a pipeline. `--fail-on` decouples the exit code from the
default severity map, and `--format` emits machine-readable reports:

```bash
queryshift inspect "$DATABASE_URL" --fail-on=critical --format=sarif > queryshift.sarif
```

`--format=sarif` produces [SARIF 2.1.0](https://sarifweb.azurewebsites.net/);
upload it with `github/codeql-action/upload-sarif` and every finding lands in your
repo's **Security** tab, linked to its catalogue page. `--format=junit` feeds
Jenkins/GitLab test panes. Suppressed findings stay visible (a SARIF suppression /
a JUnit `skipped`) and never affect the exit code.

### GitHub Action

```yaml
- uses: sarankumar1325/queryshift@v1
  with:
    dsn: ${{ secrets.QUERYSHIFT_DSN }}
    fail-on: critical
```

That runs the check and uploads SARIF to the Security tab. **The DSN must be a
`pg_monitor` role with no data access — never a superuser.** Create one:

```sql
CREATE ROLE queryshift_ci LOGIN PASSWORD '…';
GRANT pg_monitor TO queryshift_ci;
GRANT CONNECT ON DATABASE yourdb TO queryshift_ci;
```

`pg_monitor` grants read access to the statistics views queryshift needs and nothing
else — no table data. The job that uploads SARIF needs `security-events: write`.

### Reviewing a migration PR: schema profile + fail-on-new

An empty CI database has never been queried, so the full profile fires
`unused_indexes` and `stale_statistics` on everything and buries the one change
that matters. `--profile=schema` runs only the findings derivable from the catalog
(invalid/redundant indexes, unindexed FKs, a narrow `int4`/`serial` identity
column, autovacuum disabled on a table) — valid on an empty, freshly-migrated
database. Pair it with `--fail-on-new` so the check fails **only on what the PR
introduced**, not pre-existing findings:

```yaml
# .github/workflows/pr.yml — runs on every pull request
- run: |                      # 1. base branch, migrated → base report
    git checkout ${{ github.base_ref }} && ./migrate.sh
- uses: sarankumar1325/queryshift@v1
  with: { dsn: "${{ env.CI_DSN }}", profile: schema, format: json }
- run: mv queryshift-report.json base.json
- run: |                      # 2. PR branch, migrated
    git checkout ${{ github.sha }} && ./migrate.sh
- uses: sarankumar1325/queryshift@v1     # fails only on findings new vs base
  with: { dsn: "${{ env.CI_DSN }}", profile: schema, base-report: base.json, fail-on: warn }
```

(Or skip step 1 and download a `base.json` artifact your `main` CI already
produced.) This is deliberately **quiet when correct** — no PR comment, SARIF
annotations for new findings only, and the exit code carries the verdict. A check
that speaks on every PR is a check nobody reads.

The two profiles answer different questions and you want both: the schema check on
`pull_request` above, and the **full profile against production on a schedule** —

```yaml
# .github/workflows/nightly.yml
on: { schedule: [{ cron: "0 7 * * *" }] }
# ... uses: sarankumar1325/queryshift@v1 with a read-only DSN to the production replica
```

— which sees backups, replication, bloat, and wraparound that a schema check
never can. A clean `--profile=schema` report says nothing about a running
database's health; its own header says so.

### Prometheus

`--format=prometheus` writes the [node_exporter textfile
format](https://github.com/prometheus/node_exporter#textfile-collector): every
finding as a `queryshift_finding{id,severity,dimension,object}` series plus the gauges
behind them (`queryshift_cache_hit_ratio`, `queryshift_xid_age`, `queryshift_connections_used`,
`queryshift_replica_lag_seconds`, …), so an alert can fire on a trend before a finding
crosses its threshold. Suppressed findings are exported with `suppressed="true"`,
not dropped — a muted config stays visible in your metrics.

queryshift has **no daemon** — that is deliberate. Point it at a textfile collector on a
cron or systemd timer:

```bash
queryshift inspect "$DATABASE_URL" --format=prometheus > /var/lib/node_exporter/queryshift.prom.$$
mv /var/lib/node_exporter/queryshift.prom.$$ /var/lib/node_exporter/queryshift.prom   # atomic
```

Under `--all-databases`, each database's series carry a `database="…"` label;
under `--all-instances`, `instance="…"` and `role="writer|reader"` as well.

## The findings catalogue

Every finding queryshift emits has a reference page — what it observed, why it matters,
a **read-only query to verify it yourself**, how to fix it, when to ignore it (with
a pasteable `[[ignore]]` block), and what queryshift cannot see. Browse them by symptom
in [`docs/findings/`](docs/findings/README.md), or read one offline straight from
the binary:

```
queryshift explain-finding low_hot_update_ratio
```

Every line of a report tells you its id, so `queryshift explain-finding <id>` always
has the page.

## Serverless Postgres (Neon, scale-to-zero)

Scale-to-zero databases (Neon, Databricks Lakebase, and similar) **discard
in-memory statistics when the compute suspends** — by default after ~5 minutes
idle. After each wake, `pg_stat_statements` history, cache-hit counters and
index-scan counts all start again from zero.

queryshift detects this and **degrades rather than lies**:

- If the statistics were reset (or the server restarted) since the last run, the
  entire `deltas` section is suppressed with a reason — a counter going from 40M
  to 12k is a wake, not a −99.97% change.
- On a cold window (younger than 15 minutes), counter-based findings — unused
  indexes, cache-hit, sequential-scan-heavy — are suppressed, because they'd be
  meaningless or actively dangerous. Gauges (blocking chains, idle-in-transaction,
  replication lag, invalid indexes) are valid immediately and still reported.
- The report header states the window age plainly.

If you want continuous history, disable scale-to-zero or raise the suspend
timeout so the statistics survive between runs.

## Roadmap and non-goals

- **Host OS metrics** (CPU, disk IOPS, free memory) are **not** reachable over a
  SQL connection. On managed databases they live behind the provider's own API;
  on your own hardware, a future agent-on-host will read them.
- **AI is optional and explain-only.** `queryshift explain` can put a plain-language
  explanation on top of the findings (see above), but the findings themselves are
  always computed deterministically in Go — no model ever generates one. The same
  holds for `queryshift why`: its causal chains are computed from your stored snapshot
  history, never guessed.
- **queryshift never writes.** It recommends indexes; it doesn't create them.

## Troubleshooting

<details>
<summary><strong>Connecting to a local Docker database stalls for ~10 seconds</strong></summary>

Use `127.0.0.1`, not `localhost`. `localhost` resolves to IPv6 (`::1`) first,
which Docker Desktop doesn't forward, so the connect stalls before falling back
to IPv4. Managed hosts (RDS, Supabase, Neon…) aren't affected. See
[Postgres in Docker](#postgres-in-docker).

</details>

<details>
<summary><strong>"queries section unavailable"</strong></summary>

`pg_stat_statements` isn't installed or isn't in `shared_preload_libraries`.
queryshift prints the steps for your specific provider; see
[`docs/providers.md`](docs/providers.md).

</details>

<details>
<summary><strong>Findings look partial or sessions are missing</strong></summary>

The role is missing `pg_monitor`. See
[Setup](#setup--a-read-only-role-with-pg_monitor) — queryshift names the exact GRANT
at connect time.

</details>

<details>
<summary><strong>Deltas are missing on a database I've inspected before</strong></summary>

Statistics were reset or the server restarted; queryshift suppresses deltas rather
than reporting a fake −99% change. On serverless Postgres this is expected —
see [Serverless Postgres](#serverless-postgres-neon-scale-to-zero).

</details>

<details>
<summary><strong><code>npx queryshift</code> returns E404</strong></summary>

The bare npm name `queryshift` is blocked by npm's package-name-similarity rule; the
package is scoped. Use `npx @queryshift/cli`.

</details>

## Privacy

Nothing leaves the machine unless you ask for it: every command except the AI
layer is entirely local. The only commands that make an outbound call are `queryshift
explain` and `queryshift ask`, which send the same PII-free Context to your configured
model — Gemini, Anthropic, OpenAI, xAI, AWS Bedrock Mantle, or an
OpenAI-compatible endpoint — and say so, naming the provider, host, and model,
with a confirmation prompt. Bedrock's token is minted locally from your
environment credentials; queryshift never reads AWS config files or calls STS or
instance metadata. A
local endpoint (Ollama, vLLM, LM Studio on this machine) is identified as local
and sends nothing off the box.

That Context is PII-free by construction: `pg_stat_statements` text is normalized
(`$1` placeholders), and the one raw-SQL source (`pg_stat_activity` for blocking
chains) is scrubbed of string/numeric literals, emails, and UUIDs before it can
enter the Context. Connection strings are redacted in every log, error, and
output. This holds for a reader of the source, not just as a claim.

## Contributing

Issues and PRs welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for the
invariants that are load-bearing (read-only, deterministic findings, PII-free
output) and the dev loop: `go build ./cmd/queryshift`, `go test ./...`
(DB-dependent tests self-skip), and `scripts/gate.sh` before pushing.
`make matrix` runs the suite against the PostgreSQL matrix in
`docker-compose.test.yml`.

## Security

queryshift handles connection strings and reads production statistics. To report a
vulnerability, use GitHub's **private vulnerability reporting** (Security →
Report a vulnerability) — please don't open a public issue. Scope and response
expectations are in [SECURITY.md](SECURITY.md).

## License

[Apache-2.0](LICENSE).
