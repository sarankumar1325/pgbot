# OpenCode-inspired agent architecture plan

This plan is based on OpenCode `dev` at commit
[`60a5a6c`](https://github.com/anomalyco/opencode/tree/60a5a6ca7ce176b04cd2321ffd1819b1ea8e6006).
QueryShift adopts the control-plane patterns that fit a database agent; it does
not copy OpenCode's coding-specific tools or turn the database into an
unattended execution target.

## What is worth adopting

| OpenCode pattern | Source | QueryShift adaptation |
|---|---|---|
| Declarative agent profiles | [`agent.ts`](https://github.com/anomalyco/opencode/blob/60a5a6ca7ce176b04cd2321ffd1819b1ea8e6006/packages/core/src/agent.ts) | Named mapper/transpiler/validator/optimizer/judge specs instead of constructor wiring scattered through the orchestrator. |
| Ordered allow/ask/deny rules | [`permission.ts`](https://github.com/anomalyco/opencode/blob/60a5a6ca7ce176b04cd2321ffd1819b1ea8e6006/packages/core/src/permission.ts) | Last-match-wins tool rules. `ask` is fail-closed until a human approval broker exists. Database writes are never exposed as ordinary tools. |
| Typed execution lifecycle | [`session/execution.ts`](https://github.com/anomalyco/opencode/blob/60a5a6ca7ce176b04cd2321ffd1819b1ea8e6006/packages/core/src/session/execution.ts) | Append-only run/stage/retry/fallback/completion events with a run ID and monotonic sequence. |
| Failure classification and bounded retry | [`model-retry.ts`](https://github.com/anomalyco/opencode/blob/60a5a6ca7ce176b04cd2321ffd1819b1ea8e6006/packages/core/src/model-retry.ts) | Retry identified Bedrock throttling, service, timeout, and transport failures; never retry authentication, invalid requests, context overflow, or unknown errors. |
| Model-aware request budgets | [`model-request.ts`](https://github.com/anomalyco/opencode/blob/60a5a6ca7ce176b04cd2321ffd1819b1ea8e6006/packages/core/src/session/model-request.ts) | Preserve explicit Bedrock `max_tokens`, record actual usage per stage, then use measurements to size stage budgets and compaction thresholds. |
| Native Bedrock Converse transport | [`amazon-bedrock.ts`](https://github.com/anomalyco/opencode/blob/60a5a6ca7ce176b04cd2321ffd1819b1ea8e6006/packages/ai/src/providers/amazon-bedrock.ts) | Continue using Strands' Bedrock model, which uses Bedrock as the model provider, while applying adaptive Botocore retries and keeping database tools in the isolated MCP process. |

## Phase 1 — runtime control plane (implemented)

- Declarative `AgentSpec` definitions for the five specialists.
- Ordered wildcard tool permissions with `allow`, `ask`, and `deny`.
- Fail-closed behavior for `ask` tools until an approval UI is attached.
- Bedrock adaptive SDK retry mode and explicit output-token cap.
- Host-level transient failure classification and bounded jittered retry.
- Run IDs, ordered lifecycle events, stop reasons, token usage, cache usage,
  tool-call counts, and error categories in the modernization trace.
- Backward-compatible trace rendering in the Go TUI.

## Phase 2 — streaming sessions and approval broker (in progress)

1. **Implemented:** NDJSON event mode in `queryshift-modernize`; the Go process
   renders stages and retries while they happen instead of waiting for final JSON.
2. **Implemented:** sanitized run metadata and events in QueryShift's existing
   SQLite store. Never
   store connection strings, SQL literals containing sensitive values, model
   credentials, or raw chain-of-thought.
3. **Next:** add a permission-request channel from the Python host to the TUI with three
   outcomes: allow once, deny with feedback, or cancel the run.
4. **Next:** keep database mutation as a two-part capability:
   - the model may produce a typed `ChangeProposal`;
   - only the Go host can validate, preview, request confirmation, and execute it
     through the existing single-statement transaction boundary.
5. Do not implement “always allow” for database writes. Saved approvals may be
   reasonable for read-only catalog tools, never for DDL/DML.

## Phase 3 — resumability and context management

1. Persist stage checkpoints after every validated structured result.
2. Resume from the last completed stage after a process crash or retriable
   provider outage; do not rerun already accepted stages unless their inputs
   changed.
3. Track prompt size per stage and compact only evidence/history, never the
   original SQL, current candidate SQL, deterministic validator output, or
   unresolved schema mappings.
4. Use stable prompt prefixes and Bedrock prompt caching where the selected
   model supports it; measure cache-read tokens before claiming savings.
5. Add cancellation settlement so TUI cancellation waits for the subprocess and
   MCP child to close before a new run starts.

## Phase 4 — evaluation and production hardening

1. Build a golden migration corpus for Oracle, T-SQL, and DB2 with expected AST
   properties, result-set fixtures, and failure cases.
2. Score mapping accuracy, syntax validity, semantic-equivalence probes,
   retries, latency, token usage, and human rejection rate separately.
3. Add deterministic replay tests from stored sanitized events; model outputs
   should be fixtures, not live Bedrock calls in CI.
4. Add OpenTelemetry spans keyed by `run_id`, stage, model ID, and retry category.
5. Add per-stage model overrides only after evaluation shows a cheaper/faster
   model meets that stage's quality target.

## Patterns intentionally not copied

- A broad provider marketplace: QueryShift's agent stays Bedrock-first as
  requested. Database portability is independent of model-provider portability.
- General shell/file-edit tools: a database modernization agent needs narrow,
  typed database evidence tools instead.
- Autonomous writes: model output remains untrusted. Mutation requires host-side
  SQL validation, a visible preview, and explicit human confirmation.
- A client/server split before streaming and persistence require it. The local
  Go host plus isolated Python agent remains simpler for the current product.

## Acceptance criteria

- No model-visible connection string or AWS credential.
- Every agent has a deterministic effective tool list.
- Transient retries are visible, bounded, jittered, and cancelable.
- Each completed stage records model stop reason and usage.
- A process restart can resume without repeating accepted stages (Phase 3).
- No DDL/DML can execute without a TUI permission request and the existing
  per-statement confirmation boundary (Phase 2).
