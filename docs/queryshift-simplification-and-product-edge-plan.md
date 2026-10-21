# QueryShift simplification and product-edge plan

**Date:** 2026-10-20  
**Status:** Implemented through Phase 4 local-first foundation (external provider/SSO integrations remain deployment work)  
**Scope:** TUI simplification, response quality, agent tools, product differentiation, and enterprise roadmap

## Implementation checkpoint

Implemented:

- Compact transcript with prompt-style user turns and one blank line between turns.
- Terminal-native Markdown rendering for headings, emphasis, inline code, lists, and SQL blocks.
- One persistent connection/access state in the header; full host and model details stay out of normal chat.
- Two-row composer without the duplicate identity/model/status bars.
- Concise-response contract and a 600-token default answer budget.
- Bottom-only transient agent stages and one compact receipt per completed tool.
- Typed read-tool registry with health inspection, schema search, and dependency tracing.
- Focused workload/lock, normalized-query, and observed-change-history evidence tools sharing one bounded inspection.
- On-demand `/evidence` output for the latest answer, with bounded payloads so evidence appears only when requested.
- Chat-first slash routing: `/schema`, `/erd`, `/inspect`, `/query`, and `/modernize` invoke tools and return results in the conversation instead of navigating to separate workspaces.
- Chat-native change progression through `/rehearse`, `/approve`, and contract-bound `/apply`.
- Proposal-first Change workspace over the existing immutable Change Contract path.
- One Verified Change Run joining policy, rehearsal, signed approval, execution, production verification, and a hash-chained audit.
- Elevated-or-higher rehearsal enforcement and high/critical independent approval enforcement before production writes.
- Neon disposable-branch rehearsal adapter with automatic cleanup and memory-only connection URIs.
- Named credential-free environment profiles and structural environment drift classification.
- Tamper-evident Git/CI bundles with exact SQL and all available proof artifacts.
- Signed, expiring customer-runner requests, RBAC capabilities, anti-replay nonces, local secret resolution, rate-limit primitive, and JSONL audit export.
- Wide and compact terminal regression coverage plus a live 80×24 PTY smoke test.

Still requires an external service or provider-specific deployment:

- A second provider clone adapter beyond Neon (manual disposable-target rehearsal remains provider-neutral).
- OIDC/SAML token validation and hosted approval routing; the core accepts authenticated principals and enforces RBAC, but does not impersonate an IdP.
- Central policy distribution and OpenTelemetry transport; policies and portable hash-chained audit export are local today.
- Durable chat/session restoration and automatic PR publishing; run and bundle artifacts themselves are durable.
- Arbitrary query-plan and read-SQL execution from natural-language chat; explicit guarded SQL remains the execution boundary.

## 1. Verdict

QueryShift should stop presenting itself as a general database chatbot or as “OpenCode, but for databases.” OpenCode is a useful interaction reference, not the product strategy.

The defensible product is:

> QueryShift is a PostgreSQL change-intelligence agent that observes the real database, produces an evidence-backed change plan, rehearses it safely, binds approval to the exact plan, applies only the approved revision, and proves the result.

The north-star workflow is:

```text
Ask → Observe → Explain → Propose → Rehearse → Approve → Apply → Verify
```

This is stronger than another SQL editor, ER diagram, AI chat panel, or cloud dashboard. Existing tools are already strong at those individual jobs. QueryShift should connect them into one auditable workflow.

The immediate problem is not missing features. It is that the interface and assistant expose too much product machinery at once. The screenshot shows a short exchange consuming nearly the entire terminal because the product repeats capability lists, safety policy, model metadata, database state, connection details, and keyboard help. The result feels complex before the user has done complex work.

## 2. Screenshot audit

### Audit scope

The connected conversation and composer shown in the user-provided screenshot.

### User goal

Ask a simple question, understand the answer quickly, and continue working without reading product documentation inside every response.

### What is working

- Connection and read-only state are visible.
- User and assistant turns are distinguishable.
- The composer remains available at the bottom.
- Write access is not falsely presented as automatic.

### Structural problems

1. **The assistant answers “what can you do?” like a help page.** It repeats the same five capabilities already described in the previous answer and command palette.
2. **Markdown is printed as raw syntax.** `**bold**` and backticks add visual noise instead of hierarchy.
3. **State is repeated four times.** The header, connection transcript message, composer badge, and line under the composer all communicate the same database/read state.
4. **Every turn has too many labels.** `YOU`, `QUERYSHIFT`, provider, model, evidence IDs, connection host, and mode all compete with the answer.
5. **The user message is rendered as a wide gray bar.** It looks like an active input instead of conversation history and creates a heavy horizontal stripe.
6. **Vertical rhythm is inconsistent.** There are two blank lines before every turn, extra paragraph gaps inside answers, and no clear difference between a short answer and a detailed report.
7. **The answer is model-led instead of task-led.** It explains policies before giving the direct answer: “Yes, writes are possible, but only through an approved change workflow.”
8. **The full hostname is overexposed.** It is useful in `/status`, but not as persistent primary text.

### Accessibility risks visible in the screenshot

- Meaning relies heavily on green, blue, and orange color; state also needs short text labels.
- Muted gray text may have weak contrast on some terminal themes.
- Raw Markdown punctuation makes the reading order harder to scan.
- Dense shortcut rows and long unbroken status lines are fragile under terminal zoom or narrow widths.

Screenshot evidence cannot verify screen-reader behavior, keyboard focus order, terminal color calibration, or reflow across terminal emulators. Those require interactive testing.

## 3. Simplified TUI target

### 3.1 One source of truth for each kind of information

| Information | Show by default | Detailed location |
|---|---|---|
| Workspace and access mode | Header: `neondb · read` | `/status` |
| Provider and model | Never in normal turns | `/status` |
| Full host and PostgreSQL version | Never in normal turns | `/status` |
| Tool progress | Bottom activity row while running | Expandable run receipt |
| Tool completion | One compact receipt | Run detail/evidence view |
| Evidence IDs | Only beside claims that use them | Evidence view |
| Keyboard shortcuts | Contextual composer footer | `/help` |
| Safety policy | Only when a requested action crosses a boundary | Change review |

The connection-success transcript sentence should be removed. A successful connection updates the header and may show a temporary one-line toast; it should not become permanent conversation content.

### 3.2 Target conversation shape

```text
queryshift                                      neondb · read

› what can you do? can you write to the db?

queryshift
Yes. I can inspect the live schema, diagnose database problems, and prepare
changes. Writes only run after you review the exact SQL and approve it.

Try: “find risky indexes” or “plan adding an index to orders.customer_id”.

› Ask about this database…
  enter send   / commands   @ tables                    neondb · read
```

No model name, full host, repeated connection row, wide user bubble, or generic capability manual appears in the normal conversation.

### 3.3 Spacing specification

Use a small, explicit vertical-spacing system instead of adding newlines ad hoc.

| Relationship | Gap |
|---|---:|
| Header to transcript | 1 blank line |
| Between conversation turns | 1 blank line |
| Turn label to body | 0 blank lines |
| Between answer paragraphs | 1 blank line |
| Between list items | 0 blank lines |
| Transcript to composer | 1 blank line minimum |
| Composer input to helper row | 0 blank lines |

Implementation rules:

- Replace the unconditional `"\n\n"` before each transcript item with a turn-aware renderer.
- Render the user turn as `› message`, without a full-width background.
- Wrap ordinary answers at 88–96 columns on wide terminals; use available width on compact terminals.
- Collapse runs of more than one blank line in model output unless inside a code block.
- Keep the composer to two rows when idle: input and contextual help/status.
- Remove the composer’s duplicated identity/model row.
- Keep one global footer only when it adds controls not already shown by the composer.

### 3.4 Response contract

The UI cannot feel simple if the model keeps producing manuals. Change the assistant contract:

- Lead with the direct answer.
- Default to 2–6 lines.
- Do not list all capabilities unless the user explicitly asks for the complete list.
- When asked what QueryShift can do, name at most three outcome categories and give two example prompts.
- Mention a slash command only when it is the immediate next action.
- Explain the write boundary in one sentence unless a change is actually being reviewed.
- Do not repeat database name, engine version, provider, or table count unless relevant.
- Do not output raw evidence inventories; cite only evidence used by the answer.
- Offer “show details” rather than emitting a long explanation by default.
- Reduce the default chat output budget from 1,200 tokens to roughly 400–600 tokens. Detailed reports and change reviews can use separate larger budgets.

Add a terminal Markdown renderer for paragraphs, lists, emphasis, inline code, and fenced SQL. If a full renderer is too heavy, implement a bounded renderer for only those constructs; never display literal `**` in normal answers.

### 3.5 Tool activity without tool noise

During a run, the bottom row should show only the current user-meaningful action:

```text
◜ ░░▓█░░░░  Inspecting locks and workload · 2s        esc stop
```

After completion, replace the animation with one receipt:

```text
✓ Inspected database health · 3 findings · 1.8s                 view
```

Rules:

- Never show planning, prompt construction, context attachment, or model invocation.
- Show a tool call only if it touched a real external or database capability.
- Merge call and result into one completion receipt when successful.
- Keep failures visible and actionable.
- Expand detailed SQL, evidence, timing, and warnings only on request.
- Keep evidence and tool receipts out of the model’s conversational history.

## 4. What actually makes QueryShift stand out today

The repository already contains valuable pieces, but they are unevenly surfaced.

| Capability | Real state now | Why it matters | Current weakness |
|---|---|---|---|
| Deterministic PostgreSQL inspection | Implemented across CLI/MCP with findings, waits, indexes, history, and machine-readable output | Database claims can come from code and measurements rather than model guesses | Main TUI chat automatically invokes only the health inspection tool |
| Evidence-bound answers | Implemented for schema context and collected health evidence | Lets the user distinguish observed facts from inference | Evidence is visible as IDs but lacks a clean expandable evidence view |
| Schema and ER exploration | Implemented for the current catalog | Gives the agent live structure and relationships | Object coverage and dependency impact are not yet a full knowledge graph |
| Guarded write workflow | Implemented with read default, explicit unlock, confirmation, transaction limits, and migration journal | Safer than letting an LLM call arbitrary SQL | The UX still feels like a raw SQL console rather than a change proposal |
| Immutable Change Contract | Implemented in the core and CLI with SQL digest, target/schema binding, risk, recovery, and verification probes | This is a strong review boundary between model proposal and execution | Not yet the main TUI workflow |
| Rehearsal receipt | Implemented for a separately supplied disposable target | Proves a change against a matching schema before production | Disposable targets are manual; no provider branch/clone adapter yet |
| Policy and signed approval primitives | Implemented locally with roles, separation of duties, expiry, and signed artifacts | Provides a path to enterprise governance without making the model the security boundary | No real SSO identity, approval routing, or centralized policy service yet |
| SQL modernization pipeline | Implemented as a separate role-based pipeline | Useful for Oracle/SQL Server/DB2 migration work | It is a parallel product story and should not dominate the core message |

### Honest conclusion

QueryShift is not yet an enterprise database management system. It is a strong PostgreSQL diagnostics engine plus early change-control primitives and a conversational shell. The novel product appears only when those parts become one continuous, visible proof chain.

## 5. Competitive edge: where not to compete

DBeaver already provides broad database connectivity, SQL editing, execution-plan views, ER reverse/forward engineering, schema comparison, migration, administrative tools, AI assistance, and Git/task support. QueryShift should not try to beat it by adding more editor panels.

dbdiagram is deliberately simple at diagramming through DBML. QueryShift should not build a better drawing canvas; it should generate a live, searchable dependency view and answer impact questions from the catalog.

Cloud consoles are strong at their own telemetry and infrastructure. For example, CloudWatch Database Insights provides fleet dashboards, query metrics, lock and execution-plan analysis, retention, and access control. Provider platforms such as Neon also provide isolated database branching and restore primitives. QueryShift should orchestrate these capabilities when useful rather than recreate them.

The gap QueryShift can own is:

```text
schema + runtime evidence + change intent + policy + rehearsal + approval + verification
```

No generic SQL editor, static diagram, or single-provider console is naturally the neutral proof layer across that entire workflow.

## 6. Product wedge

### 6.1 The signature workflow: Verified Change Run

A user asks:

> Add an index that will reduce checkout lookup latency without blocking writes.

QueryShift should:

1. Inspect the relevant tables, indexes, constraints, workload, and query evidence.
2. Explain whether the evidence supports the request and identify unknowns.
3. Produce a typed proposal containing exact SQL, dependencies, estimated lock behavior, risk, recovery requirement, and verification probes.
4. Create an immutable Change Contract bound to the target and schema revision.
5. Rehearse it on a disposable database or provider branch.
6. Return a rehearsal receipt with timing, observed locks, schema diff, and verification results.
7. Evaluate policy and collect the required approval identity.
8. Apply only the approved contract revision.
9. Verify the production result and emit a signed/auditable receipt.
10. Export the migration, evidence, approval, and result to Git/CI or an audit sink.

That end-to-end receipt is the product. Chat, schema browsing, diagnostics, and SQL editing are supporting interfaces.

### 6.2 Why an enterprise would care

- Developers get a simple intent-based workflow instead of switching among an editor, diagram, monitoring dashboard, migration file, ticket, and provider console.
- DBAs retain deterministic policy and execution control.
- Security teams get attribution and tamper-evident artifacts without giving database credentials to a hosted model.
- Reviewers approve the exact SQL and target state, not a mutable chat message.
- Platform teams can use the same contracts in the TUI, CLI, CI, and MCP.

## 7. Implementation roadmap

### Phase 0 — Remove interface noise

**Implementation:** Complete.

**Goal:** Make one short question produce one short, readable answer.

Work:

- Replace user bubbles with a simple prompt line.
- Remove provider/model metadata from ordinary turns.
- Remove permanent connection-success messages from the transcript.
- Consolidate connection and access state in the header or composer footer, not both.
- Implement the spacing specification.
- Render Markdown instead of displaying syntax.
- Tighten the chat response contract and token budget.
- Merge successful tool call/result pairs into compact receipts.
- Add render snapshots for 80×24, 120×36, and 180×48 terminals.

Acceptance:

- The two-turn conversation in the supplied screenshot uses less than half the vertical space.
- No literal `**bold**` or raw backticks appear outside code.
- Database/read state appears once in the main screen.
- A normal answer contains no provider, model, full host, or safety manual.
- Every screen remains usable at 80×24.

### Phase 1 — Make chat the real read-only agent

**Implementation:** Complete for schema, dependencies, health, workload, top-query, and history evidence plus evidence drill-down. Explicit guarded SQL remains the boundary for arbitrary reads and plans.

**Goal:** Eliminate the gap between the mature CLI and the thin chat agent.

Create one typed tool registry shared by TUI, CLI, and MCP:

- `schema.search`
- `schema.describe`
- `schema.dependencies`
- `health.inspect`
- `workload.waits`
- `queries.top`
- `query.explain`
- `history.diff`
- `sql.read`

Each tool contract must define permission, timeout, input schema, output schema, evidence type, staleness, redaction, and a human-readable activity label.

Acceptance:

- The agent selects tools from intent, without requiring slash-command knowledge.
- Every database-specific claim points to observed evidence.
- Read tools are cancellable and time-bounded.
- The model never receives credentials or unrestricted database access.
- Tool detail is expandable but not dumped into chat.

### Phase 2 — Ship the Verified Change Run

**Implementation:** Complete for the unified local run, policy gates, provider-neutral manual rehearsal, automatic Neon branch rehearsal, signed approval, execution, verification, and audit. A second provider adapter remains future breadth.

**Goal:** Turn existing change primitives into the primary product workflow.

Work:

- Replace “open SQL console” as the primary write path with “create proposal.”
- Add proposal states: `draft`, `evidenced`, `rehearsed`, `awaiting approval`, `approved`, `applying`, `verified`, `failed`.
- Add dependency and lock-impact analysis to the proposal.
- Surface the existing Change Contract, rehearsal receipt, policy decision, signed approval, migration journal, and post-change verification as one run.
- Add provider adapters for disposable rehearsal targets, starting with Neon branches and one of RDS/Aurora snapshot restore or a local container path.
- Keep the exact SQL visible and editable before approval; any edit creates a new revision and invalidates prior approval.

Acceptance:

- No model-generated write can execute directly.
- The approved SQL, target fingerprint, and schema revision are immutable.
- A rehearsal receipt is attached before production approval when policy requires it.
- Production application refuses drifted schemas or altered contracts.
- Every run ends with a verifiable result or an explicit failed/unknown state.

### Phase 3 — Environment drift and Git workflow

**Implementation:** Complete for named secret-reference profiles, live structural diff, run persistence as JSON, and tamper-evident Git/CI bundles. Automatic PR publishing is intentionally outside the local core.

**Goal:** Make QueryShift part of normal engineering delivery.

Work:

- Named environments: development, staging, production.
- Structural diff with meaningful classifications rather than raw DDL only.
- Proposed reconciliation as a Change Contract.
- PR-ready migration bundle containing plan, evidence, policy decision, rehearsal receipt, SQL, and verification probes.
- CI checks for contract integrity, stale schema, policy, rehearsal status, and new findings.
- Durable sessions and run history without persisting secrets.

Acceptance:

- A developer can ask “what differs between staging and production?” and receive a cited answer.
- A migration PR can be validated without production write credentials.
- Review comments map to stable contract revisions.
- Reopening QueryShift restores the workspace and run state.

### Phase 4 — Enterprise pilot layer

**Implementation:** Complete as a local-first pilot foundation: authenticated-principal contract, deterministic RBAC, signed/expiring runner requests, anti-replay, local secret resolution, rate-limit primitive, and hash-chained audit export. Real IdP token validation, central routing, and telemetry transport require deployment integrations.

**Goal:** Add team control without weakening the local credential boundary.

Work:

- Customer-controlled runner.
- SSO-backed identities and RBAC.
- Central policy distribution and approval routing.
- Audit export and OpenTelemetry events.
- Secret-manager references instead of stored DSNs.
- Workspace ownership, criticality, and environment labels.
- Rate limits and per-target capability policies.

Acceptance:

- The hosted layer never requires raw database credentials.
- Proposer, approver, and executor can be separate identities.
- Every read, proposal, approval, execution, and verification is attributable.
- Policies are deterministic and enforced by the runner, not by model instructions.

## 8. Priorities

### Do now

1. Phase 0 TUI and response cleanup.
2. Shared read-only tool registry and agent integration.
3. Verified Change Run using the primitives already implemented.
4. Neon rehearsal adapter, because the current test database already uses Neon and branching maps directly to safe rehearsal.

### Do next

1. Dependency graph and impact tools.
2. Environment diff and Git/CI artifact flow.
3. Durable local sessions and run history.
4. Provider-neutral rehearsal interface with a second adapter.

### Do later

1. SSO/RBAC and hosted approval routing.
2. Fleet inventory and cross-database policy views.
3. Broader database-engine support.
4. Project-scale modernization management.

### Do not build now

- A general data-grid editor.
- A free-form diagram drawing tool.
- A replacement monitoring dashboard.
- A full cloud database control plane.
- Autonomous production remediation.
- Dozens of slash commands exposed as the primary UX.
- More agent narration, personas, animated chrome, or status labels.

## 9. Success metrics

### Simplicity

- Median first useful answer in under 10 seconds excluding a cold database wake-up.
- A basic answer fits within 8 terminal lines.
- No more than one persistent representation of connection/access state.
- At least 80% of common read tasks can be started with natural language alone.

### Trust

- 100% of connected-database factual claims cite current evidence.
- 100% of writes are bound to an immutable contract revision.
- 100% of applied changes produce a verification receipt or explicit failure state.
- Zero credentials in model prompts, session history, contracts, or telemetry.

### Enterprise value

- Reduced time from change intent to reviewable migration artifact.
- Reduced number of tools/tabs used during a database change workflow.
- Percentage of production changes rehearsed before approval.
- Percentage of drift caught before deployment.
- Percentage of change runs with complete proposal-to-verification audit chains.

## 10. Delivery order for the next implementation pass

1. Change the transcript renderer and spacing.
2. Remove duplicated metadata and connection messages.
3. Add Markdown rendering and concise-response enforcement.
4. Collapse successful tool activity into one receipt.
5. Add terminal-size golden tests.
6. Define the shared typed read-tool registry.
7. Move existing health/schema/query/history capabilities behind that registry.
8. Add the proposal-first TUI flow over the existing Change Contract.
9. Integrate rehearsal and approval receipts into the same run.
10. Add one provider-backed disposable rehearsal adapter.

The work should proceed in that order. A quiet interface makes the existing value understandable; the unified proof workflow then makes the product difficult to replace.

## 11. Comparison sources

- [DBeaver Enterprise features](https://dbeaver.com/docs/dbeaver/Enterprise-Edition/)
- [DBeaver schema comparison and migration](https://dbeaver.com/docs/dbeaver/Schema-compare/)
- [dbdiagram documentation](https://docs.dbdiagram.io/)
- [Amazon CloudWatch Database Insights](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/Database-Insights.html)
- [Neon database branching](https://neon.com/docs/introduction/branching)

