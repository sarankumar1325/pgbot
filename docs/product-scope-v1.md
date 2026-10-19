# QueryShift Product Scope and Delivery Plan

**Status:** Proposed  
**Date:** 2026-10-19  
**Product stage:** Beta to focused v1  
**Primary target:** PostgreSQL 14–18  
**Working product category:** AI-native database operations and schema-change workspace

---

## 1. Executive summary

QueryShift should become the database equivalent of an agentic coding workspace: a local-first environment where an engineer can connect to a PostgreSQL database, ask questions, inspect grounded evidence, understand schema and runtime behavior, propose a change, review its impact, approve execution, and verify the result.

The product should not initially position itself as a universal enterprise database management system. That phrase implies a much larger surface—fleet administration, identity management, backup orchestration, high availability, alerting, governance, and support for many database engines. QueryShift can become enterprise-ready before it becomes a full enterprise control plane.

The recommended positioning is:

> QueryShift is an AI-native PostgreSQL workspace for understanding, diagnosing, changing, and modernizing databases safely through conversation.

SQL modernization remains an important capability, but it is a mode within the workspace rather than the entire product identity.

The north-star interaction is:

```text
Connect
  → Ask
  → Inspect grounded evidence
  → Propose a typed plan
  → Review SQL, impact, risk, and recovery
  → Approve
  → Apply within a safety boundary
  → Verify
  → Save a durable checkpoint and audit record
```

The first product milestone is not more commands. It is unifying the existing diagnostics engine, MCP tools, schema workspace, guarded write path, and modernization agents into this single loop.

---

## 2. Product vision

### 2.1 Long-term vision

QueryShift becomes the default interface between engineers and operational databases: as approachable as a modern coding agent, but grounded in database evidence and constrained by database-specific safety controls.

An engineer should be able to express intent in plain language:

- “Why did checkout become slower after yesterday’s migration?”
- “Show everything that depends on `orders.customer_id`.”
- “Plan a safe conversion of this column from `integer` to `bigint`.”
- “Compare staging and production and explain the unexpected drift.”
- “Translate this Oracle query and prove what still needs human verification.”
- “Prepare an index change, but do not execute it.”

QueryShift should convert that intent into transparent, reviewable, evidence-backed work. The model may reason and propose. Deterministic code must collect evidence, enforce policy, classify risk, execute approved operations, and verify outcomes.

### 2.2 Near-term mission

Build the safest and easiest local PostgreSQL agent workspace for individual engineers and small platform teams.

The v1 product must be excellent at five things:

1. Connecting to a database without leaking credentials.
2. Answering operational and schema questions using real tools and cited evidence.
3. Exploring a complete database schema and its dependencies.
4. Planning and applying controlled schema changes with explicit approval.
5. Preserving a durable, inspectable record of every agent run and database change.

### 2.3 Product promise

Every important answer or action must be one of the following:

- **Observed:** read directly from the database or QueryShift’s history.
- **Computed:** derived deterministically from observed evidence.
- **Inferred:** model reasoning that is explicitly labeled and linked to evidence.
- **Proposed:** a change that has not been executed.
- **Applied:** a host-confirmed action with an execution and verification record.

The interface must never blur these states.

---

## 3. Product positioning

### 3.1 Recommended category

**AI-native database operations and schema-change workspace**

This category is broad enough to contain QueryShift’s existing capabilities without promising the entire surface of a database management platform.

### 3.2 Recommended one-line description

> Understand, diagnose, change, and modernize PostgreSQL safely from one agentic terminal workspace.

### 3.3 Product modes

QueryShift should present four primary modes while retaining one conversational surface:

| Mode | User outcome | Default permission |
|---|---|---|
| Explore | Understand schema, dependencies, objects, and relationships | Read-only |
| Diagnose | Explain health, workload, regressions, waits, and likely causes | Read-only |
| Change | Plan, review, apply, and verify schema or controlled data changes | Plan-only until explicit approval |
| Modernize | Translate legacy SQL and migration assets into PostgreSQL artifacts | Offline or read-only validation |

These are not separate products. They are capabilities of one connected workspace.

### 3.4 What QueryShift is not in v1

QueryShift v1 is not:

- A replacement for pganalyze, PMM, Datadog, Grafana, or continuous monitoring platforms.
- A backup service or disaster-recovery orchestrator.
- A high-availability or failover controller.
- A universal GUI for every database engine.
- An autonomous production change bot.
- A database hosting service.
- A general-purpose shell or coding agent with unrestricted tools.
- A team SaaS control plane that stores customer database credentials.

---

## 4. Target users and jobs to be done

### 4.1 Primary users

#### Application engineer

Needs to understand a schema, investigate a slow feature, validate a query, and prepare safe database changes without becoming a full-time DBA.

Core jobs:

- Discover where data lives.
- Understand relationships and constraints.
- Diagnose query or transaction problems.
- Review the likely impact of a migration.
- Generate a change artifact for code review.

#### Database or platform engineer

Needs fast, trustworthy evidence across databases, repeatable operational workflows, and strong controls around production changes.

Core jobs:

- Triage incidents.
- Find workload regressions.
- Review schema and index proposals.
- Compare environments.
- Enforce policy and approvals.
- Produce an audit trail.

#### Migration engineer

Needs to translate legacy database logic to PostgreSQL while tracking uncertainty and proving semantic compatibility.

Core jobs:

- Map source entities to target entities.
- Translate dialect-specific SQL.
- Identify unsupported behavior.
- Generate verification probes.
- Package reviewed migration artifacts.

### 4.2 Secondary users

- Security engineers reviewing database access and change history.
- Engineering managers reviewing migration risk and completion.
- AI coding agents consuming QueryShift through MCP and versioned JSON.
- CI pipelines checking schema health and migration regressions.

### 4.3 Primary jobs-to-be-done statements

- When a production database behaves unexpectedly, help me reach a grounded explanation quickly without deploying another monitoring stack.
- When I need to change a schema, help me understand dependencies and operational risk before anything runs.
- When I approve a change, apply only the reviewed version and prove what happened afterward.
- When environments drift, show the meaningful differences and prepare a controlled reconciliation plan.
- When modernizing legacy SQL, separate verified transformations from assumptions that still need human review.

---

## 5. Product principles

### 5.1 Evidence before explanation

The agent should call tools before making database-specific claims. Answers should link back to findings, schema objects, snapshots, plans, or query results.

### 5.2 Read-only is the default state

Connecting a database must not imply permission to mutate it. Exploration, diagnostics, and agent reasoning use a read-only capability set by default.

### 5.3 Plans are not actions

The interface must make proposed, approved, executing, applied, failed, and verified states visually and structurally distinct.

### 5.4 The model is never the security boundary

The host application owns:

- Tool allowlists.
- SQL parsing and classification.
- Policy evaluation.
- Permission prompts.
- Execution.
- Timeouts and transaction boundaries.
- Audit persistence.
- Verification.

### 5.5 Local-first, enterprise-capable

The default experience should remain easy to install and run locally. Enterprise capabilities should add identity, policy, collaboration, and audit without requiring credentials to leave the customer environment.

### 5.6 Progressive disclosure

A new user should be able to connect and ask a question immediately. Detailed plans, SQL, evidence, tool traces, and policy decisions should be available when needed rather than overwhelming the first screen.

### 5.7 No false certainty

QueryShift must distinguish:

- Catalog proof.
- Planner evidence.
- Runtime measurement.
- Historical correlation.
- Model inference.
- Human confirmation.

Confidence scores must not be presented as proof of semantic equivalence or production safety.

---

## 6. Current product assessment

### 6.1 Strong foundations to preserve

- Deterministic PostgreSQL findings implemented outside the model.
- A versioned, PII-aware JSON contract.
- A substantial MCP tool surface.
- Read-only transaction controls and restricted diagnostic roles.
- Historical snapshots, diffs, events, and causal analysis.
- Schema and ER inspection.
- A guarded single-statement write path.
- Local migration artifacts with before/after schema snapshots.
- A role-separated SQL modernization pipeline.
- Agent permissions, retries, lifecycle events, and bounded execution.
- A conversational TUI with command discovery, queued prompts, and cancellation.
- CLI, CI, SARIF, Prometheus, and agent integration surfaces.

### 6.2 Main gaps

#### Fragmented product identity

The product is described as a SQL modernizer, a point-in-time diagnostic, and a database workspace. The documentation also contains conflicting write-safety promises.

#### The primary TUI chat is not yet a tool-using agent

The assistant receives schema context and produces text, but it cannot use the mature MCP tools from the main conversation. Users must know which slash command or separate agent executable to use.

#### Changes are SQL statements rather than durable change sets

The current write path is a useful safety prototype, but it lacks dependency impact, AST-based classification, lock analysis, deployment strategy, verified backup evidence, and integrated post-change tests.

#### Workspace state is not durable enough

Connections, conversations, change plans, and agent checkpoints need a cohesive persistence model.

#### The visible schema model is incomplete

The current ER-focused view does not expose all object types and dependency relationships required for enterprise schema work.

#### Enterprise governance is mostly future work

The local product lacks team identity, role separation, approval routing, centralized audit export, secret-manager references, and policy bundles.

#### Modernization operates on one query at a time

Enterprise migrations require project-level inventory, mapping, batch progress, shared decisions, verification datasets, and exportable artifacts.

---

## 7. North-star user experience

### 7.1 First-run experience

The first screen should offer one clear entry point: the conversation composer.

Suggested empty-state prompts:

- Connect a PostgreSQL database.
- Open an existing workspace.
- Modernize SQL without a live connection.
- Learn what QueryShift can do.

The user should not need to understand the command catalog before receiving value.

### 7.2 Connected workspace

After connection, the header should always show:

- Workspace name.
- Environment classification.
- Database and host alias.
- Connection capability: read-only or change-capable.
- Agent/model status.
- Current mode.
- Whether a write capability is armed.

The connection string must never be shown after submission.

### 7.3 Conversational tool loop

For a database-specific question, the interface should show a compact execution timeline:

```text
● Reading recent database snapshots
● Inspecting query statistics
● Comparing schema events
● Analyzing orders-related regressions
✓ 4 evidence sources collected
```

The answer should contain evidence references that open the relevant schema object, finding, query, snapshot, or plan.

### 7.4 Change-planning loop

When a user asks for a change, the agent should create a draft change set rather than opening an unrestricted write console.

The draft should display:

- Intent summary.
- Proposed SQL.
- Affected objects.
- Dependency impact.
- Lock and rewrite risk.
- Estimated data impact.
- Transactional behavior.
- Required privileges.
- Deployment approach.
- Verification steps.
- Rollback or recovery strategy.
- Policy decisions.
- Unresolved questions.

The user can edit or reject the plan before approval.

### 7.5 Approval loop

The approval screen should make the exact action obvious:

- Which environment will change.
- Which database will change.
- Which SQL hash is approved.
- Which identity approved it.
- Which policy gates passed or failed.
- Whether a verified recovery point exists.
- What post-change checks will run.

The approval must bind to the exact immutable change-set revision. Editing SQL invalidates the prior approval.

### 7.6 Verification loop

After execution, QueryShift should automatically perform safe verification:

- Confirm transaction completion.
- Capture the resulting schema.
- Compare expected and actual schema changes.
- Re-run selected diagnostics.
- Run explicit validation queries.
- Record duration, rows affected, and warnings.
- Mark the change as verified, applied-with-warnings, failed, or verification-pending.

### 7.7 Session and history

Users should be able to reopen a workspace and see:

- Prior conversations.
- Agent tool traces.
- Saved findings.
- Change-set history.
- Modernization runs.
- Environment comparisons.
- Pending approvals.
- Failed or incomplete verification.

---

## 8. Core product model

### 8.1 Workspace

A workspace is the top-level local unit of work.

Required fields:

- Stable workspace ID.
- Human-readable name.
- Database engine and version.
- One or more connection profile references.
- Environment labels.
- Repository path, when applicable.
- Policy profile.
- Model profile.
- Default read-only capability.
- Local persistence directory.

Credentials are never stored in the workspace document. It stores only a reference such as a `PGSERVICE` name, environment variable name, keychain item, or secret-manager locator.

### 8.2 Connection profile

A connection profile describes how QueryShift obtains a credential and what capabilities are expected.

Fields:

- Profile ID and display name.
- Environment.
- Host alias and database name.
- Credential source reference.
- Provider.
- Expected role.
- Read-only, plan-only, or change-capable classification.
- TLS requirements.
- SSH or network access configuration.
- Last successful verification.

### 8.3 Session

A session is a durable conversation and execution timeline inside a workspace.

Fields:

- Session ID.
- Workspace ID.
- Created and updated timestamps.
- Conversation turns.
- Tool events.
- Evidence references.
- Active and completed plans.
- Checkpoints.
- Cancellation and failure state.
- Sanitized model usage metadata.

### 8.4 Evidence object

Every database-specific claim should be traceable to an evidence object.

Evidence types include:

- Catalog snapshot.
- Health finding.
- Query statistic.
- Query plan.
- Baseline comparison.
- Causal chain.
- Schema event.
- Environment diff.
- Read-query result.
- Provider metadata.
- Verification result.

Evidence records include source, collection time, exactness, freshness, truncation, and access limitations.

### 8.5 Change set

A change set is the canonical unit of mutation.

Required lifecycle:

```text
draft
  → analyzed
  → policy_blocked | ready_for_review
  → approved
  → executing
  → applied
  → verifying
  → verified | applied_with_warnings | verification_pending

Any pre-commit failure → failed and rolled_back
Any user cancellation before execution → cancelled
```

Required fields:

- Change-set ID and immutable revision.
- Workspace and target profile.
- User intent.
- Proposed SQL statements.
- Parsed AST and normalized SQL hash.
- Affected objects.
- Risk classification.
- Policy results.
- Required approvals.
- Recovery requirements.
- Pre-change snapshot.
- Execution events.
- Post-change snapshot.
- Verification specification and results.
- Exported migration paths.

### 8.6 Modernization project

A modernization project groups many source assets and shared decisions.

Required fields:

- Source engine and version.
- Target PostgreSQL version.
- Source assets and inventory.
- Source-to-target entity mappings.
- Global transformation decisions.
- Per-asset status.
- Unresolved questions.
- Validation and equivalence results.
- Performance comparison.
- Human review decisions.
- Export artifacts.

---

## 9. Functional scope

## 9.1 Explore mode

### V1 requirements

- Search objects by name and type.
- Browse schemas, tables, columns, indexes, constraints, sequences, and extensions.
- Display foreign-key relationships.
- Display object definitions without exposing table data.
- Show object size and selected operational metadata.
- Link objects to diagnostics and historical changes.
- Open the dependency graph for an object.
- Ask natural-language questions about visible schema evidence.
- Copy qualified identifiers and definitions safely.

### Required schema expansion

Add catalog support for:

- Views.
- Materialized views.
- Functions and procedures.
- Triggers.
- RLS policies.
- Roles, ownership, and grants as metadata.
- Enums, domains, and composite types.
- Partitions and inheritance.
- Generated columns.
- Defaults with a redacted/controlled representation.
- Publications and subscriptions.
- Comments.
- Object dependencies from PostgreSQL catalogs.

### Later requirements

- Column-level data classification.
- Ownership annotations.
- Links to application-code references.
- Business glossary integration.
- Cross-database lineage.

## 9.2 Diagnose mode

### V1 requirements

- Integrate the existing MCP diagnostic tools into the main conversation.
- Automatically choose relevant read-only tools.
- Show tool activity and cancellation state.
- Attach evidence references to answers.
- Preserve deterministic findings unchanged.
- Clearly label model inference.
- Support follow-up questions using prior evidence.
- Detect stale evidence and refresh it when necessary.
- Allow the user to pin an evidence snapshot to a session.

### Agent tool policy

Diagnostic agents may use only allowlisted read-only tools by default.

The initial allowlist should include:

- Inspect.
- Schema catalog.
- Schema object details.
- Top queries.
- Vacuum health.
- Wait analysis.
- Baseline comparison.
- Causal analysis.
- Safe planner explanation.
- Finding documentation.
- Index analysis.

Read queries against application tables require a separate permission because they can expose customer data even when they cannot mutate it.

## 9.3 Change mode

### V1 requirements

- Create change sets from natural-language intent or pasted SQL.
- Parse PostgreSQL SQL into an AST.
- Reject unsupported or ambiguous statements.
- Detect multiple statements explicitly.
- Identify affected objects and dependencies.
- Classify destructive, blocking, rewriting, non-transactional, and privilege-sensitive operations.
- Estimate lock levels and table rewrite behavior.
- Estimate row impact for controlled DML where possible.
- Generate a deployment plan.
- Generate verification steps.
- Generate rollback SQL only when it is provably appropriate.
- Require recovery evidence when rollback cannot restore data.
- Bind approval to the normalized SQL hash and revision.
- Execute through the Go host only.
- Capture before/after schema and verification results.
- Export a migration artifact suitable for Git review.

### V1 supported changes

Start with a conservative set:

- Create table.
- Add nullable column.
- Add column with reviewed safe default strategy.
- Create index, preferring concurrent creation when appropriate.
- Add or validate constraints through staged plans.
- Create or replace views when dependency analysis is available.
- Comments on schema objects.
- Controlled inserts and updates where row impact can be previewed.

### Restricted changes

Require elevated policy and stronger recovery evidence:

- Drop table, column, schema, or type.
- Truncate.
- Broad update or delete.
- Type changes that can rewrite a table or lose information.
- Constraint validation on large tables.
- Index operations with significant locking implications.
- Changes on production environments.

### Out of scope for initial change mode

- Role and user administration.
- Database creation or deletion.
- Server configuration changes.
- Extension installation requiring superuser access.
- Multi-database transactions.
- Automatic failover or backup operations.
- Unattended production execution.

## 9.4 Environment diff and drift

### V1 requirements

- Register development, staging, and production profiles in one workspace.
- Capture normalized schema snapshots.
- Compare two environments.
- Classify expected, unmanaged, and ignored differences.
- Generate a reconciliation change set.
- Store explicit drift exceptions with owner, reason, and expiry.
- Prevent application of a plan generated from a stale target snapshot.
- Show the source snapshot and target snapshot used by every plan.

### Drift categories

- Missing object.
- Unexpected object.
- Changed definition.
- Ownership or grant difference.
- Extension-version difference.
- Index difference.
- Constraint difference.
- RLS-policy difference.
- Unverifiable difference due to permissions.

## 9.5 Modernize mode

### V1 requirements

- Preserve the existing single-query modernization workflow.
- Allow offline modernization without a live database connection.
- Support optional target schema snapshots.
- Support read-only target catalog grounding.
- Surface unresolved mappings before translation proceeds.
- Preserve validation issues and verification probes.
- Export structured reports and SQL artifacts.
- Label planner-validated and unverified recommendations separately.

### Next-stage requirements

- Import a directory or manifest of SQL assets.
- Inventory queries, views, procedures, and functions.
- Reuse project-level entity mappings.
- Track batch progress and failures.
- Detect duplicated transformation decisions.
- Support human-approved mapping rules.
- Run equivalence probes against controlled source and target datasets.
- Compare query plans and performance budgets.
- Export an ordered migration package.

### Engine scope

For the near term:

- PostgreSQL is the only managed target database.
- Oracle, SQL Server, DB2, MySQL, and Sybase are source dialects for modernization.
- Live source connectors should arrive only when QueryShift has a clear credential, data-access, and verification policy for each engine.

---

## 10. Agent architecture

### 10.1 Unified runtime goal

The TUI, standalone diagnostic agent, and modernizer should share one agent runtime contract.

The runtime must support:

- Declarative agent profiles.
- Ordered allow/ask/deny permissions.
- Typed tool schemas.
- Streaming lifecycle events.
- Cancellation.
- Bounded retries.
- Model and token budgets.
- Durable checkpoints.
- Sanitized run persistence.
- Deterministic replay tests.
- Provider-neutral model interfaces where practical.

### 10.2 Agent roles

#### Workspace agent

The primary conversational agent. It interprets intent, selects read-only tools, summarizes evidence, and creates typed proposals.

#### Diagnostic specialist

Analyzes runtime evidence and deterministic findings. It cannot generate new findings or mutate the database.

#### Schema specialist

Explores objects and dependencies, explains schema structure, and contributes impact analysis.

#### Change planner

Produces a typed `ChangeProposal`. It never executes SQL.

#### Policy and risk evaluator

Runs deterministic rules over the proposed plan, target environment, permissions, and recovery evidence.

#### Execution host

Trusted Go code, not an LLM role. It validates the approved immutable plan and performs the exact authorized operation.

#### Verification specialist

Selects or explains post-change checks, but deterministic host code executes and records those checks.

#### Modernization specialists

Continue using mapper, transpiler, validator, optimizer, and judge roles, integrated through the shared runtime contract.

### 10.3 Permission model

Every tool has:

- Capability class.
- Data sensitivity.
- Mutation level.
- Environment restrictions.
- Default decision: allow, ask, or deny.
- Optional policy conditions.

Suggested capability classes:

| Capability | Default |
|---|---|
| Read public catalog structure | Allow |
| Read normalized operational statistics | Allow |
| Read application table rows | Ask or deny |
| Run plain `EXPLAIN` | Allow |
| Run `EXPLAIN ANALYZE` | Ask and policy-controlled |
| Create a change proposal | Allow |
| Apply DDL/DML | Ask every time |
| Server, role, or database administration | Deny in v1 |

No permanent “always allow” option should exist for database mutation.

### 10.4 Context management

Never compact or omit:

- Current user intent.
- Target workspace and environment.
- Current plan revision.
- Proposed SQL.
- Deterministic validator results.
- Unresolved schema mappings.
- Policy failures.
- Approval state.

Evidence history may be summarized only when references to the original evidence remain available.

---

## 11. Safety and trust architecture

### 11.1 Connection separation

Use separate named capabilities instead of one ambiguous connection:

- `catalog_reader`
- `diagnostic_reader`
- `data_reader`
- `change_executor`

A workspace may have only the first two configured. The interface must not imply that write capability exists when it does not.

### 11.2 SQL validation

Replace keyword-only classification for change planning with a PostgreSQL-aware parser and AST policy engine.

Validation stages:

1. Parse.
2. Confirm statement count.
3. Normalize.
4. Identify object references.
5. Classify operation and lock behavior.
6. Evaluate transaction compatibility.
7. Detect unsafe patterns.
8. Evaluate environment policy.
9. Generate immutable plan revision and hash.

### 11.3 Recovery evidence

Distinguish these states:

- Recovery not required.
- Reversible by reviewed down migration.
- Transaction rollback available before commit.
- Verified backup or PITR point available.
- User claims a backup exists but QueryShift cannot verify it.
- Recovery unavailable.

Never record “backup acknowledged” merely because a backup is required. Store the actor, time, evidence source, and verification status.

### 11.4 Production safety

Production policies should support:

- Read-only by default.
- Strong visual environment identity.
- Mandatory plan freshness.
- Mandatory approval from a separate identity.
- Maintenance-window requirements.
- Maximum estimated rows affected.
- Maximum lock timeout.
- Maximum statement timeout.
- Required backup evidence for destructive changes.
- Required post-change verification.
- Immediate cancellation before commit where technically possible.

### 11.5 Audit record

Every material operation should record:

- Actor.
- Workspace and target.
- Session ID.
- Intent.
- Plan revision and SQL hash.
- Evidence snapshot IDs.
- Policy results.
- Approval identity and timestamp.
- Execution start and end.
- Database response.
- Before and after schema hashes.
- Verification results.
- Failure and recovery state.

Local audit records should be append-only and checksum-protected. Enterprise export may target object storage, OpenTelemetry, or SIEM systems.

---

## 12. Enterprise readiness

### 12.1 Enterprise-ready local edition

Deliver before a hosted control plane:

- Committable workspace policy.
- Credential references rather than stored secrets.
- Deterministic audit export.
- Environment classification.
- Change-set approval files or signed approval records.
- Git-friendly migration artifacts.
- Offline and air-gapped operation where model configuration permits.
- Configurable model and data-residency settings.
- Clear capability and access reporting.

### 12.2 Team control plane

Add only after the local workflow is stable.

Capabilities:

- OIDC/SAML SSO.
- Organizations and teams.
- RBAC.
- Workspace registry.
- Approval routing.
- Central policy distribution.
- Central audit collection.
- Fleet metadata and ownership.
- Session sharing with redacted evidence.
- Managed agent updates.

The control plane should not require raw database credentials. Execution should occur in a customer-controlled local agent or runner.

### 12.3 Fleet management

Later enterprise scope:

- Database inventory.
- Labels, teams, owners, and criticality.
- Read-only parallel inspection.
- Cross-database schema standards.
- Fleet-wide drift reporting.
- Extension and version inventory.
- Upgrade-readiness assessments.
- Policy compliance summaries.

Fleet-wide write execution is explicitly excluded until per-target plans, approvals, rate limits, and rollback controls are mature.

---

## 13. Interfaces

### 13.1 TUI

The TUI is the primary interactive product.

Requirements:

- Conversation-first home.
- Persistent workspace context.
- Tool execution timeline.
- Evidence drawer or panel.
- Object explorer.
- Change-set review screen.
- Approval screen.
- Run and audit history.
- Keyboard-first interaction.
- Compact-terminal support.
- Accessible state communication that does not rely only on color.

Slash commands remain discoverable accelerators, not required knowledge.

### 13.2 CLI

The CLI remains the automation and expert interface.

Add cohesive commands around the product model:

```text
queryshift workspace list
queryshift workspace open <name>
queryshift ask "..."
queryshift plan <request-or-file>
queryshift plan show <id>
queryshift apply <id>
queryshift verify <id>
queryshift drift <source> <target>
queryshift history
queryshift modernize <path>
```

Existing diagnostic commands should remain backward compatible.

### 13.3 MCP

MCP remains the integration surface for external agents.

Separate MCP servers or capability profiles should exist for:

- Read-only diagnostics.
- Schema exploration.
- Change proposal creation.

Direct mutation tools should not be exposed to general external agents in v1. External agents may create a proposal; the QueryShift host owns review and execution.

### 13.4 CI

CI use cases:

- Schema linting.
- Migration plan analysis.
- Policy checking.
- Drift detection.
- Fail-on-new findings.
- Modernization fixture evaluation.
- Verification of generated migration artifacts.

CI must never require production write credentials.

---

## 14. Data, privacy, and model boundaries

### 14.1 Default data policy

The model may receive:

- Schema structure allowed by policy.
- Normalized query text.
- Deterministic findings.
- Redacted operational evidence.
- User-provided SQL.
- Explicitly approved read-query results.

The model must not receive by default:

- Connection strings.
- Credentials.
- AWS credentials.
- Raw application table rows.
- Unredacted logs.
- Secrets embedded in defaults, functions, or comments.
- Raw chain-of-thought.

### 14.2 Model provider strategy

Keep a provider-neutral language-model interface in the Go host. Certify a narrow set of models rather than promising universal provider parity.

For enterprise deployments, expose:

- Provider and model ID.
- Region.
- Data-residency implications.
- Whether prompt caching is enabled.
- Whether requests leave the local machine or cloud boundary.
- Per-session usage and cost metadata where available.

### 14.3 Evidence retention

Retention should be configurable by evidence class:

- Schema snapshots.
- Diagnostic snapshots.
- Read-query results.
- Model transcripts.
- Agent events.
- Change-set records.
- Audit exports.

Read-query results should have the shortest default retention and may be disabled entirely.

---

## 15. Technical architecture direction

### 15.1 Go host as trusted control plane

The Go process remains responsible for:

- Database connections.
- Secret boundary enforcement.
- Catalog and diagnostic collection.
- Tool registration.
- Policy evaluation.
- Plan validation.
- Permission requests.
- Execution.
- Persistence.
- Audit.
- TUI and CLI surfaces.

### 15.2 Agent runtime isolation

The Python/Strands runtime may remain as a subprocess during the focused v1 phase, provided the protocol becomes a stable internal contract.

The protocol must include:

- Run start.
- Stage start/completion/failure.
- Tool request and result metadata.
- Permission request.
- Typed proposal.
- Checkpoint.
- Cancellation acknowledgement.
- Final structured result.

Credentials must remain separated between model and database processes.

### 15.3 Persistence

Use the existing local SQLite store as the initial persistence layer.

Add tables for:

- Workspaces.
- Connection-profile references.
- Sessions.
- Conversation turns.
- Evidence records.
- Tool events.
- Change sets and revisions.
- Approvals.
- Verification results.
- Modernization projects and assets.

Large artifacts should live as content-addressed files with hashes stored in SQLite.

### 15.4 Repository integration

When a workspace is linked to a Git repository:

- Export migrations into a configurable directory.
- Record the repository commit used for analysis.
- Detect uncommitted migration edits.
- Preserve stable change-set IDs in artifact metadata.
- Generate review summaries suitable for pull requests.
- Never commit or push automatically without an explicit future workflow.

---

## 16. Delivery roadmap

## Milestone 0 — Product truth and terminology

### Goal

Make the product promise consistent before adding more surface area.

### Deliverables

- Replace conflicting README positioning.
- Define Explore, Diagnose, Change, and Modernize modes.
- Document PostgreSQL as the managed target.
- Document legacy engines as modernization sources.
- Reconcile “never writes” language with the guarded change mode.
- Make offline modernization available without a database connection.
- Align confirmation copy with actual required confirmation tokens.
- Add a capability matrix covering TUI, CLI, MCP, and agent surfaces.

### Acceptance criteria

- A new reader can explain the product in one sentence.
- Every write-related claim matches actual behavior.
- Documentation clearly distinguishes implemented, beta, experimental, and planned features.

## Milestone 1 — Durable workspace and session model

### Goal

Create the persistent foundation required for an OpenCode-like experience.

### Deliverables

- Workspace configuration format.
- Named connection-profile references.
- Environment labels.
- Persistent session records.
- Conversation history.
- Tool-event persistence.
- Workspace and session CLI commands.
- Reopen and resume behavior in the TUI.

### Acceptance criteria

- Restarting QueryShift restores the selected workspace and session history.
- No credentials are written to workspace or session storage.
- A user can switch between two named databases without re-entering non-secret metadata.

## Milestone 2 — Unified tool-using conversational agent

### Goal

Make the main TUI conversation the primary agent interface.

### Deliverables

- Integrate the read-only MCP tools into the main chat runtime.
- Stream tool lifecycle events into the transcript.
- Add evidence references.
- Add per-tool permissions.
- Add stale-evidence detection.
- Preserve queued prompts and cancellation.
- Remove the need to run the separate diagnostic agent for ordinary use.

### Acceptance criteria

- “Why is this database slow?” triggers relevant diagnostic tools.
- Database-specific claims link to collected evidence.
- The agent cannot access database credentials or mutation capabilities.
- Tool use is cancelable and bounded.

## Milestone 3 — Schema knowledge graph

### Goal

Support dependable impact analysis and richer schema exploration.

### Deliverables

- Expanded object catalog.
- Dependency graph.
- Object search.
- Object detail view.
- Historical object changes.
- Agent tools for dependency and impact queries.

### Acceptance criteria

- QueryShift can answer what depends on a selected table, column, view, or function.
- Permission-limited objects are reported as unavailable rather than absent.
- Large schemas remain searchable without inserting the entire schema into model context.

## Milestone 4 — Typed change sets and safe execution

### Goal

Replace raw-statement mutation as the primary workflow.

### Deliverables

- Change-set domain model.
- PostgreSQL AST parser integration.
- Risk and lock classifier.
- Dependency impact analysis.
- Immutable plan revisions.
- Permission-request channel.
- Approval binding.
- Verified recovery evidence model.
- Automatic post-change verification.
- Git-friendly migration export.

### Acceptance criteria

- The model can propose but cannot execute a change.
- The host refuses SQL that differs from the approved revision.
- Editing a plan invalidates its approval.
- Destructive production changes cannot proceed without required policy and recovery evidence.
- Every applied change has before/after state and verification status.

## Milestone 5 — Environment diff and drift

### Goal

Make QueryShift useful across the development-to-production lifecycle.

### Deliverables

- Multiple environment profiles per workspace.
- Normalized schema comparison.
- Drift classification.
- Expiring drift exceptions.
- Reconciliation plan generation.
- Stale-snapshot protection.

### Acceptance criteria

- Users can compare staging and production from the TUI and CLI.
- QueryShift separates missing, unexpected, changed, ignored, and unverifiable differences.
- Reconciliation generates a reviewable change set rather than executing directly.

## Milestone 6 — Modernization projects

### Goal

Expand from single-query conversion to a managed migration workflow.

### Deliverables

- Project manifest.
- Directory or bundle import.
- Asset inventory.
- Shared entity mappings.
- Batch run orchestration.
- Human decision records.
- Equivalence test harness.
- Migration package export.

### Acceptance criteria

- A migration project can resume without repeating accepted work.
- Shared mappings are reused consistently.
- Every asset exposes validation, risk, and review state.
- Verification results distinguish semantic, syntax, and performance confidence.

## Milestone 7 — Enterprise control plane

### Goal

Add team governance without weakening the local credential boundary.

### Deliverables

- Customer-controlled runner.
- SSO and RBAC.
- Workspace registry.
- Policy distribution.
- Approval routing.
- Central audit export.
- Fleet inventory and ownership.
- OpenTelemetry integration.

### Acceptance criteria

- The hosted service does not require raw database credentials.
- Production approvals can require a separate identity.
- All execution remains attributable and policy-controlled.
- Fleet reads and changes respect target-specific rate and capability limits.

---

## 17. Prioritized backlog

### P0 — Required for the focused product

- Unify product language and navigation.
- Persist workspaces and sessions.
- Integrate the tool-using agent into the TUI.
- Add evidence references to agent answers.
- Add typed change proposals.
- Add AST-based SQL classification.
- Add immutable plan revisions.
- Add approval broker.
- Correct backup acknowledgement semantics.
- Add automatic post-change verification.
- Make offline modernization work from the home screen.

### P1 — Required for enterprise pilots

- Complete schema object coverage.
- Dependency graph.
- Environment profiles.
- Schema drift detection.
- Policy-as-code.
- Git migration export.
- Secret-manager references.
- Audit export.
- Deterministic agent replay tests.
- Modernization evaluation corpus.

### P2 — Scale and team workflows

- Team identities and RBAC.
- Separate proposer, approver, and executor roles.
- Customer-controlled runners.
- Fleet inventory.
- Cross-database reporting.
- Batch modernization projects.
- Provider API integrations for verified backups and infrastructure metadata.

### P3 — Future expansion

- Live legacy-source connectors.
- Broader engine-specific diagnostics.
- Application-code dependency indexing.
- Business glossary and ownership systems.
- Cross-database lineage.
- Controlled fleet remediation.

---

## 18. Evaluation and testing strategy

### 18.1 Deterministic engine tests

- PostgreSQL version matrix.
- Managed-provider compatibility tests.
- Permission-degradation tests.
- Read-only guarantee tests.
- Schema dependency fixtures.
- Risk-classification fixtures.
- Transaction and rollback tests.
- Stale-plan rejection tests.
- Audit integrity tests.

### 18.2 Agent evaluation

Maintain versioned evaluation suites for:

- Tool selection accuracy.
- Unsupported-claim rate.
- Evidence citation correctness.
- Plan completeness.
- Risk identification.
- Policy compliance.
- Human rejection rate.
- Retry behavior.
- Latency and token usage.
- Prompt-injection resistance from SQL comments, identifiers, and catalog text.

Model outputs used in CI should be fixtures or deterministic replays rather than live provider calls.

### 18.3 Modernization evaluation

Build a golden corpus for Oracle, T-SQL, DB2, MySQL, and Sybase.

Measure separately:

- Parse success.
- Mapping accuracy.
- Syntax validity.
- Catalog validity.
- Semantic equivalence.
- Null and duplicate behavior.
- Ordering and cardinality behavior.
- Transaction semantics.
- Performance change.
- Human acceptance.

### 18.4 UX testing

Test these complete flows:

- First launch to first grounded answer.
- Reconnect to an existing workspace.
- Diagnose a query regression.
- Explore an object dependency chain.
- Create and reject a change plan.
- Approve, apply, and verify a safe change.
- Recover from a failed change.
- Compare staging and production.
- Resume an interrupted modernization project.

Test at minimum terminal sizes and verify keyboard navigation, focus, screen-reader output where supported, reduced-motion behavior, and non-color state indicators.

---

## 19. Success metrics

### 19.1 Activation

- Percentage of users who connect or start offline modernization successfully.
- Time from install to first grounded answer.
- Connection failure rate by provider.
- Percentage of first sessions that use at least one evidence tool.

### 19.2 Diagnostic value

- Time to first relevant evidence.
- Percentage of answers with valid evidence references.
- Unsupported database-specific claim rate.
- Follow-up success without repeating setup.
- User-rated usefulness of recommended next steps.

### 19.3 Change safety

- Percentage of proposed changes blocked or revised before execution.
- Percentage of applied changes with complete verification.
- Approval invalidation correctness after edits.
- Failed execution rollback success.
- Applied-without-verification rate.
- Recovery-evidence coverage for destructive changes.

### 19.4 Modernization quality

- Syntax-valid output rate.
- Catalog-valid output rate.
- Semantic-equivalence pass rate.
- Human acceptance rate.
- Average unresolved mappings per asset.
- Rework rate after verification.

### 19.5 Product simplicity

- Percentage of tasks completed from the main conversation without discovering a separate executable.
- Commands required per successful workflow.
- Session resume success rate.
- Time to switch environments safely.
- Documentation searches per completed core task.

### 19.6 Enterprise readiness

- Percentage of operations attributable to an identity.
- Policy-evaluation coverage.
- Audit export completeness.
- Percentage of credentials supplied through references rather than pasted DSNs.
- Production changes with separate approval.

---

## 20. Risks and mitigations

### Risk: Product scope expands too quickly

**Mitigation:** Keep PostgreSQL as the managed target and make the local single-workspace loop excellent before fleet or multi-engine administration.

### Risk: The conversational interface appears more capable than it is

**Mitigation:** Make tool use visible, label inferred claims, and never present a proposal as an applied action.

### Risk: Users interpret confidence as safety proof

**Mitigation:** Replace single confidence scores with explicit evidence categories and named unresolved risks.

### Risk: Schema context becomes too large

**Mitigation:** Use search and targeted schema tools. Never place the entire enterprise schema directly in the prompt.

### Risk: Change automation damages production

**Mitigation:** Human approval, immutable revisions, deterministic policy, short transactions, lock timeouts, recovery evidence, and post-change verification.

### Risk: Local audit history is lost or edited

**Mitigation:** Content hashes, append-only records, optional Git export, and enterprise remote audit export.

### Risk: Provider-specific behavior undermines generic plans

**Mitigation:** Maintain provider capability profiles and surface uncertainty when provider metadata is unavailable.

### Risk: Modernization output passes syntax checks but changes behavior

**Mitigation:** Verification probes, controlled source/target datasets, result comparison, and explicit human review gates.

### Risk: Data leaks through tools or prompts

**Mitigation:** Capability-based data access, redaction, short retention, explicit permission for row reads, prompt-injection defenses, and provider transparency.

---

## 21. Product decisions required

The team should explicitly decide and record:

1. Is the v1 product PostgreSQL-only for managed targets? Recommended: yes.
2. Is SQL modernization a mode or the primary product identity? Recommended: a mode.
3. Can the agent ever execute a write directly? Recommended: no; it creates proposals only.
4. Will production approval require a separate identity in enterprise mode? Recommended: yes.
5. Will QueryShift store credentials? Recommended: no; store references only.
6. Will application row reads be available to the agent? Recommended: only through an explicit, policy-controlled permission.
7. Is the initial enterprise architecture local runner plus optional control plane? Recommended: yes.
8. Are monitoring and alerting in scope? Recommended: integrations and scheduled snapshots only, not a monitoring-platform replacement.
9. Are role and server administration in v1? Recommended: no.
10. What is the stable compatibility contract? Recommended: versioned JSON, typed internal events, migration artifacts, and MCP schemas.

---

## 22. Definition of v1

QueryShift v1 is complete when an engineer can:

1. Install one primary application.
2. Create or reopen a workspace.
3. Connect to PostgreSQL using a credential reference or ephemeral DSN.
4. Ask a database question in the main conversation.
5. Watch the agent use allowlisted read-only tools.
6. Open the evidence behind the answer.
7. Explore complete schema objects and dependencies.
8. Ask for a schema change.
9. Receive a typed, immutable, policy-evaluated change set.
10. Review SQL, impact, risk, recovery, and verification.
11. Explicitly approve the exact revision.
12. Apply the change through the trusted host.
13. See the actual before/after state and verification result.
14. Reopen the session and audit record after restarting QueryShift.
15. Export the migration artifact for Git review.
16. Modernize legacy SQL offline or against the connected target schema.

The product should not be called generally available for production change management until destructive-change recovery, approval separation, audit integrity, and real provider restoration drills have been validated.

---

## 23. Immediate execution sequence

The recommended order of work is:

1. Correct positioning and contradictory documentation.
2. Define the workspace, session, evidence, and change-set schemas.
3. Persist and reopen workspaces and sessions.
4. Integrate read-only MCP tools into the main conversation.
5. Add evidence references and visible tool execution.
6. Expand the schema catalog and dependency graph.
7. Implement typed change proposals and AST validation.
8. Add the approval broker and immutable plan revisions.
9. Add automatic verification and correct recovery evidence.
10. Add environment comparison and drift workflows.
11. Expand modernization into project-level work.
12. Introduce the enterprise runner and team control plane only after the local loop is stable.

This sequence protects the core product idea: QueryShift should feel simple because it unifies a complex database workflow, not because it hides risk or skips necessary controls.
