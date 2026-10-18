# QueryShift agents

An optional [Strands Agents](https://strandsagents.com/) host that uses Amazon
Bedrock for reasoning and queryshift's existing MCP server for deterministic,
read-only PostgreSQL evidence. It provides two terminal applications and Python
SDK entry points:

- `queryshift-agent` — interactive database diagnosis.
- `queryshift-modernize` — a five-agent legacy SQL to PostgreSQL pipeline.

Bedrock is only the model provider. The target database can be Amazon RDS or
Aurora, Neon, Supabase, Google Cloud SQL, Azure Database for PostgreSQL, or any
self-hosted PostgreSQL instance that queryshift can reach.

## Run

Prerequisites:

- Python 3.11+
- `uv`
- `queryshift` on `PATH` (or `QUERYSHIFT_BIN=/path/to/queryshift`)
- AWS credentials permitted to invoke the selected Bedrock model
- `DATABASE_URL` or `PGSERVICE` for a read-only PostgreSQL role

```bash
export DATABASE_URL='postgres://queryshift_ro:...@host:5432/app?sslmode=require'
export AWS_REGION=us-east-1
export QUERYSHIFT_AGENT_MODEL='deepseek.v3.2'

uv run --project agent queryshift-agent 'Why is this database slow?'
```

For local development, copy the repository-root `.env.example` to `.env` and
fill in your AWS credentials. Both terminal applications load that file
automatically; variables already exported by your shell take precedence. Never
commit `.env`.

## Enterprise Query Modernizer

The modernizer is an orchestrated pipeline, not an unbounded group chat:

```text
Schema Mapper → Dialect Transpiler → Validator ↔ Repair → Optimizer → Judge
```

It currently accepts one read-only Oracle, T-SQL, DB2, MySQL, Sybase, or
PostgreSQL query and targets PostgreSQL. The output is a structured modernization
package: mapped entities, translated SQL, validation findings, optimized SQL,
index proposals, a unified diff, risk/confidence metadata, verification probes,
and a stage timing trace.

Offline with a target schema snapshot:

```bash
uv run --project agent queryshift-modernize legacy.sql \
  --source oracle \
  --schema-file target-schema.sql \
  --output modernization-report.json
```

Grounded against a live PostgreSQL catalog:

```bash
export DATABASE_URL='postgres://queryshift_ro:...@host:5432/app?sslmode=require'
go build -o ./queryshift ./cmd/queryshift

QUERYSHIFT_BIN="$PWD/queryshift" uv run --project agent queryshift-modernize legacy.sql \
  --source tsql \
  --live-schema \
  --format json
```

Interactive hosts can select `--format ndjson`. It emits one flushed lifecycle
event per line and one terminal result record, allowing the Go TUI to render the
active specialist, retries, timings, and completion state while Bedrock is
running. These progress records contain metadata only—not SQL, prompts, database
credentials, or model reasoning.

Live mode gives only `schema_catalog`, `schema_of`, and safe `explain_plan` to
the specialists. The catalog tools read structure, not application rows, and
`explain_plan` uses plain `EXPLAIN`, never `EXPLAIN ANALYZE`. The command does
not execute the migrated query and does not apply index DDL.

Index impact stays `unverified` unless planner evidence exists. The confidence
score is a transparent heuristic—not a proof of semantic equivalence. The judge
emits source- and target-side verification probes that must be run on equivalent
data snapshots before production approval.

## Runtime architecture

The agent control plane borrows a few deliberately narrow patterns from
OpenCode's open-source runtime:

- five declarative agent profiles rather than five ad-hoc constructors;
- ordered `allow` / `ask` / `deny` tool rules, with `ask` failing closed until a
  human approval broker is connected;
- ordered lifecycle events carrying a run ID, stage attempts, retries, stop
  reasons, token usage, cache usage, and tool-call counts;
- adaptive Bedrock SDK retries plus bounded host retries only for positively
  identified transient failures;
- an NDJSON subprocess protocol for live TUI progress and sanitized run history
  in QueryShift's local SQLite state store.

The complete roadmap and source comparison is in
[`docs/opencode-agent-architecture-plan.md`](../docs/opencode-agent-architecture-plan.md).

Python SDK usage:

```python
from queryshift_agent import AgentConfig
from queryshift_agent.modernizer import run_modernization

report = run_modernization(
    "SELECT TOP (10) * FROM [dbo].[Orders]",
    AgentConfig(),
    source_dialect="tsql",
    target_schema="CREATE TABLE orders (...);",
)
print(report.optimization.sql)
```

During repository development, point the agent at a locally built binary:

```bash
go build -o ./queryshift ./cmd/queryshift
QUERYSHIFT_BIN="$PWD/queryshift" uv run --project agent queryshift-agent \
  'What changed and what should I investigate first?'
```

The agent process starts `queryshift mcp` over stdio. Database credentials remain in
the subprocess environment and are not placed in the model prompt. Only
allowlisted read-only tools are exposed. `record_index_verdict`, which writes to
queryshift's local store, is deliberately excluded.

Bedrock credentials stay in the Strands host and are stripped from the queryshift
tool subprocess. The database connection stays in the queryshift subprocess and its
`connection_string` argument is removed from the tool schemas shown to the model.

## Configuration

| Variable | Default | Purpose |
|---|---:|---|
| `QUERYSHIFT_AGENT_MODEL` | `deepseek.v3.2` | Bedrock model or inference-profile ID |
| `AWS_REGION` / `AWS_DEFAULT_REGION` | `us-east-1` | Bedrock inference region |
| `QUERYSHIFT_BIN` | `queryshift` | queryshift executable used for MCP |
| `QUERYSHIFT_AGENT_MAX_TURNS` | `8` | Maximum reason/tool cycles |
| `QUERYSHIFT_AGENT_MAX_TOKENS_PER_TURN` | `4096` | Bedrock output cap for each model call |
| `QUERYSHIFT_BEDROCK_SDK_MAX_ATTEMPTS` | `5` | Botocore adaptive retry attempts |
| `QUERYSHIFT_AGENT_MAX_TRANSIENT_RETRIES` | `2` | Host-level retries for classified transient failures |
| `QUERYSHIFT_AGENT_RETRY_BASE_SECONDS` | `0.5` | Initial host retry delay before jitter |
| `QUERYSHIFT_AGENT_RETRY_MAX_SECONDS` | `8` | Maximum host retry delay |
| `QUERYSHIFT_AGENT_MAX_TOTAL_TOKENS` | `30000` | Whole-invocation input + output budget |
| `QUERYSHIFT_AGENT_MAX_OUTPUT_TOKENS` | `8000` | Whole-invocation generated-token budget |
| `QUERYSHIFT_AGENT_TIMEOUT_SECONDS` | `180` | Wall-clock deadline for one invocation |
| `QUERYSHIFT_MODERNIZER_MAX_REPAIRS` | `2` | Validator-to-transpiler repair attempts |
| `QUERYSHIFT_MODERNIZER_MAX_SQL_CHARS` | `100000` | Maximum SQL or schema-snapshot input size |

Use the exact model or inference-profile ID available to your AWS account and
region. Geographic (`us.`, `eu.`, `apac.`) and global profiles have different
data-residency implications.

## Test

```bash
uv run --project agent --group dev pytest
```

The unit tests need no database and make no Bedrock call. A live smoke test uses
the normal run command after AWS credentials and `DATABASE_URL` are configured.
