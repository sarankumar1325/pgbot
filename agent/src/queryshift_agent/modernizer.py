"""Bounded five-agent workflow for legacy-query modernization."""

from __future__ import annotations

import difflib
import json
import time
from collections.abc import Callable, Sequence
from contextlib import nullcontext
from threading import Event, Timer
from typing import Any, TypeVar

import sqlglot
from pydantic import BaseModel
from sqlglot import exp
from sqlglot.errors import ParseError
from strands import Agent

from .agent_specs import AgentSpec, ToolPermission
from .config import MODERNIZER_TOOLS, AgentConfig
from .events import EventRecorder, EventSink
from .modernizer_models import (
    DiffSummary,
    JudgeAssessment,
    MappingResult,
    ModernizationReport,
    OptimizationResult,
    TraceEvent,
    TranspileResult,
    ValidationIssue,
    ValidationResult,
)
from .modernizer_prompts import (
    JUDGE_PROMPT,
    MAPPER_PROMPT,
    OPTIMIZER_PROMPT,
    TRANSPILER_PROMPT,
    VALIDATOR_PROMPT,
)
from .runtime import build_mcp_client, build_model
from .resilience import classify_failure, retry_delay

T = TypeVar("T", bound=BaseModel)


MODERNIZER_SPECS = {
    "mapper": AgentSpec(
        name="schema-semantic-mapper",
        description="Maps legacy identifiers to evidenced target entities",
        system_prompt=MAPPER_PROMPT,
        permissions=(
            ToolPermission("*", "deny"),
            ToolPermission("schema_catalog", "allow"),
            ToolPermission("schema_of", "allow"),
        ),
    ),
    "transpiler": AgentSpec(
        name="dialect-transpiler",
        description="Translates legacy query syntax to PostgreSQL",
        system_prompt=TRANSPILER_PROMPT,
    ),
    "validator": AgentSpec(
        name="ast-validator",
        description="Checks syntax, catalog conformance, and semantic risks",
        system_prompt=VALIDATOR_PROMPT,
        permissions=(
            ToolPermission("*", "deny"),
            ToolPermission("schema_of", "allow"),
            ToolPermission("explain_plan", "allow"),
        ),
    ),
    "optimizer": AgentSpec(
        name="performance-optimizer",
        description="Produces semantics-preserving PostgreSQL optimizations",
        system_prompt=OPTIMIZER_PROMPT,
        permissions=(
            ToolPermission("*", "deny"),
            ToolPermission("schema_of", "allow"),
            ToolPermission("explain_plan", "allow"),
        ),
    ),
    "judge": AgentSpec(
        name="equivalence-judge",
        description="Audits semantic risk and creates verification probes",
        system_prompt=JUDGE_PROMPT,
    ),
}


def static_validate_postgres(sql: str) -> list[ValidationIssue]:
    """Parse one read-only PostgreSQL statement and return deterministic issues."""

    issues: list[ValidationIssue] = []
    try:
        statements = sqlglot.parse(sql, read="postgres")
    except ParseError as exc:
        return [
            ValidationIssue(
                severity="error",
                code="postgres_parse_error",
                message=str(exc),
            )
        ]

    if len(statements) != 1:
        issues.append(
            ValidationIssue(
                severity="error",
                code="multiple_statements",
                message="Modernizer output must contain exactly one SQL statement.",
            )
        )
    if not statements:
        issues.append(
            ValidationIssue(
                severity="error",
                code="empty_sql",
                message="Modernizer output did not contain a SQL statement.",
            )
        )
        return issues

    root = statements[0]
    write_nodes: Sequence[type[exp.Expression]] = (
        exp.Insert,
        exp.Update,
        exp.Delete,
        exp.Create,
        exp.Drop,
        exp.Alter,
        exp.Merge,
    )
    if any(root.find(node) is not None for node in write_nodes):
        issues.append(
            ValidationIssue(
                severity="error",
                code="write_statement",
                message="Only read-only query modernization is supported.",
            )
        )
    if root.find(exp.Star) is not None:
        issues.append(
            ValidationIssue(
                severity="warning",
                code="select_star",
                message="SELECT * can make migrations sensitive to schema drift.",
            )
        )
    return issues


def _payload(**values: Any) -> str:
    return json.dumps(values, indent=2, ensure_ascii=False, default=str)


class Modernizer:
    """Orchestrates five role-separated Strands agents with one repair loop."""

    def __init__(
        self,
        config: AgentConfig,
        *,
        role_tools: dict[str, list[Any]] | None = None,
        cancel_signal: Event | None = None,
        event_sink: EventSink | None = None,
        sleep: Callable[[float], None] | None = None,
    ) -> None:
        self.config = config
        self.cancel_signal = cancel_signal or Event()
        self.events = EventRecorder(event_sink)
        self._sleep = sleep
        role_tools = role_tools or {}
        model = build_model(config)
        common = {"model": model, "callback_handler": None}
        agents: dict[str, Agent] = {}
        for role, spec in MODERNIZER_SPECS.items():
            available = {
                tool.tool_name: tool for tool in role_tools.get(role, [])
            }
            agents[role] = Agent(
                name=spec.name,
                description=spec.description,
                system_prompt=spec.system_prompt,
                tools=spec.resolve_tools(available),
                **common,
            )
        self.mapper = agents["mapper"]
        self.transpiler = agents["transpiler"]
        self.validator = agents["validator"]
        self.optimizer = agents["optimizer"]
        self.judge = agents["judge"]
        self.trace: list[TraceEvent] = []

    def _invoke(
        self,
        stage: str,
        agent: Agent,
        prompt: str,
        output_model: type[T],
        *,
        attempt: int = 1,
    ) -> T:
        for transport_attempt in range(1, self.config.max_transient_retries + 2):
            if self.cancel_signal.is_set():
                raise TimeoutError("modernization workflow was cancelled or timed out")
            started = time.monotonic()
            self.events.emit(
                "stage_started",
                stage=stage,
                logical_attempt=attempt,
                transport_attempt=transport_attempt,
                agent=agent.name,
            )
            try:
                result = agent(
                    prompt,
                    structured_output_model=output_model,
                    limits={
                        "turns": self.config.max_turns,
                        "total_tokens": self.config.max_total_tokens,
                        "output_tokens": self.config.max_output_tokens,
                    },
                    cancel_signal=self.cancel_signal,
                    invocation_state={
                        "run_id": self.events.run_id,
                        "stage": stage,
                        "logical_attempt": attempt,
                        "transport_attempt": transport_attempt,
                    },
                )
                if result.structured_output is None:
                    raise RuntimeError(f"{stage} returned no structured output")
                output = output_model.model_validate(result.structured_output)
            except Exception as exc:
                failure = classify_failure(exc)
                duration_ms = int((time.monotonic() - started) * 1000)
                self.trace.append(
                    TraceEvent(
                        stage=stage,
                        attempt=attempt,
                        transport_attempt=transport_attempt,
                        agent=agent.name,
                        status="failed",
                        duration_ms=duration_ms,
                        summary=f"{type(exc).__name__}: {exc}",
                        error_category=failure.category,
                        retryable=failure.retryable,
                    )
                )
                self.events.emit(
                    "stage_failed",
                    stage=stage,
                    logical_attempt=attempt,
                    transport_attempt=transport_attempt,
                    agent=agent.name,
                    duration_ms=duration_ms,
                    error_type=type(exc).__name__,
                    error_category=failure.category,
                    retryable=failure.retryable,
                )
                can_retry = (
                    failure.retryable
                    and not self.cancel_signal.is_set()
                    and transport_attempt <= self.config.max_transient_retries
                )
                if not can_retry:
                    raise
                delay = retry_delay(
                    transport_attempt,
                    base_seconds=self.config.retry_base_seconds,
                    max_seconds=self.config.retry_max_seconds,
                )
                self.trace.append(
                    TraceEvent(
                        stage=stage,
                        attempt=attempt,
                        transport_attempt=transport_attempt,
                        agent=agent.name,
                        status="retry",
                        duration_ms=0,
                        summary=f"Retrying transient {failure.category} in {delay:.2f}s",
                        error_category=failure.category,
                        retryable=True,
                    )
                )
                self.events.emit(
                    "retry_scheduled",
                    stage=stage,
                    logical_attempt=attempt,
                    transport_attempt=transport_attempt,
                    agent=agent.name,
                    delay_seconds=delay,
                    error_category=failure.category,
                )
                if self._sleep is not None:
                    self._sleep(delay)
                elif self.cancel_signal.wait(delay):
                    raise TimeoutError("modernization workflow was cancelled")
                continue

            usage = getattr(result.metrics, "accumulated_usage", {}) or {}
            tool_metrics = getattr(result.metrics, "tool_metrics", {}) or {}
            tool_calls = sum(
                getattr(metric, "call_count", 0) for metric in tool_metrics.values()
            )
            duration_ms = int((time.monotonic() - started) * 1000)
            trace = TraceEvent(
                stage=stage,
                attempt=attempt,
                transport_attempt=transport_attempt,
                agent=agent.name,
                status="completed",
                duration_ms=duration_ms,
                summary=_stage_summary(output),
                stop_reason=result.stop_reason,
                input_tokens=usage.get("inputTokens", 0),
                output_tokens=usage.get("outputTokens", 0),
                total_tokens=usage.get("totalTokens", 0),
                cache_read_tokens=usage.get("cacheReadInputTokens", 0),
                cache_write_tokens=usage.get("cacheWriteInputTokens", 0),
                tool_calls=tool_calls,
            )
            self.trace.append(trace)
            self.events.emit(
                "stage_completed",
                stage=stage,
                logical_attempt=attempt,
                transport_attempt=transport_attempt,
                agent=agent.name,
                duration_ms=duration_ms,
                stop_reason=result.stop_reason,
                input_tokens=trace.input_tokens,
                output_tokens=trace.output_tokens,
                total_tokens=trace.total_tokens,
                tool_calls=tool_calls,
            )
            return output
        raise RuntimeError(f"{stage} exhausted its retry budget")

    def run(
        self,
        sql: str,
        *,
        source_dialect: str,
        target_schema: str | None = None,
        live_schema: bool = False,
    ) -> ModernizationReport:
        """Run the workflow and always publish a terminal lifecycle event."""

        try:
            return self._run(
                sql,
                source_dialect=source_dialect,
                target_schema=target_schema,
                live_schema=live_schema,
            )
        except Exception as exc:
            failure = classify_failure(exc)
            self.events.emit(
                "run_failed",
                error_type=type(exc).__name__,
                error_category=failure.category,
                retryable=failure.retryable,
            )
            raise

    def _run(
        self,
        sql: str,
        *,
        source_dialect: str,
        target_schema: str | None = None,
        live_schema: bool = False,
    ) -> ModernizationReport:
        if not sql.strip():
            raise ValueError("legacy SQL is empty")
        if len(sql) > self.config.max_sql_chars:
            raise ValueError(
                f"legacy SQL exceeds {self.config.max_sql_chars} character limit"
            )
        if target_schema and len(target_schema) > self.config.max_sql_chars:
            raise ValueError(
                f"target schema exceeds {self.config.max_sql_chars} character limit"
            )

        self.events.emit(
            "run_started",
            source_dialect=source_dialect,
            live_schema=live_schema,
            model_id=self.config.model_id,
        )

        mapping = self._invoke(
            "mapper",
            self.mapper,
            _payload(
                task="Map legacy entities to the PostgreSQL target schema.",
                source_dialect=source_dialect,
                legacy_sql=sql,
                target_schema_snapshot=target_schema or "not supplied",
                live_schema_tools_available=live_schema,
                live_schema_instruction=(
                    "Call schema_catalog to inspect target entities before mapping."
                    if live_schema
                    else "No database is connected; do not claim catalog validation."
                ),
            ),
            MappingResult,
        )

        transpilation = self._invoke(
            "transpiler",
            self.transpiler,
            _payload(
                task="Transpile the legacy query to one PostgreSQL query.",
                source_dialect=source_dialect,
                legacy_sql=sql,
                mapping=mapping.model_dump(),
            ),
            TranspileResult,
        )

        repair_attempts = 0
        validation = self._validate(sql, mapping, transpilation, live_schema)
        while (
            not validation.valid and repair_attempts < self.config.max_repair_attempts
        ):
            repair_attempts += 1
            transpilation = self._invoke(
                "transpiler_repair",
                self.transpiler,
                _payload(
                    task="Repair the PostgreSQL query using validator feedback.",
                    source_dialect=source_dialect,
                    legacy_sql=sql,
                    mapping=mapping.model_dump(),
                    previous_transpilation=transpilation.model_dump(),
                    validator_feedback=validation.model_dump(),
                ),
                TranspileResult,
                attempt=repair_attempts,
            )
            validation = self._validate(
                sql, mapping, transpilation, live_schema, attempt=repair_attempts + 1
            )

        if not validation.valid:
            raise RuntimeError(
                "transpiled SQL did not pass validation after "
                f"{repair_attempts} repair attempt(s)"
            )

        optimization = self._invoke(
            "optimizer",
            self.optimizer,
            _payload(
                task="Optimize the validated PostgreSQL query without semantic drift.",
                legacy_sql=sql,
                mapping=mapping.model_dump(),
                validated_transpilation=transpilation.model_dump(),
                validation=validation.model_dump(),
                live_schema_tools_available=live_schema,
            ),
            OptimizationResult,
        )
        optimized_issues = static_validate_postgres(optimization.sql)
        if any(issue.severity == "error" for issue in optimized_issues):
            optimization.cautions.append(
                "Optimizer output failed deterministic PostgreSQL parsing; using the "
                "validated transpiler output instead."
            )
            optimization.sql = transpilation.sql
            self.trace.append(
                TraceEvent(
                    stage="optimizer_guard",
                    status="fallback",
                    duration_ms=0,
                    summary="Rejected invalid optimizer SQL and restored validated SQL",
                )
            )
            self.events.emit(
                "fallback_applied",
                stage="optimizer_guard",
                reason="invalid optimizer SQL",
            )

        judge = self._invoke(
            "judge",
            self.judge,
            _payload(
                task="Audit the modernization and create cross-engine verification probes.",
                source_dialect=source_dialect,
                legacy_sql=sql,
                mapping=mapping.model_dump(),
                transpilation=transpilation.model_dump(),
                validation=validation.model_dump(),
                optimization=optimization.model_dump(),
                repair_attempts=repair_attempts,
            ),
            JudgeAssessment,
        )

        diff = build_diff(sql, optimization.sql)
        score, basis = confidence_score(
            mapping, validation, judge, repair_attempts, live_schema
        )
        risk = risk_level(score, mapping, validation, judge)
        self.events.emit(
            "run_completed",
            risk_level=risk,
            confidence_score=score,
            repair_attempts=repair_attempts,
        )
        return ModernizationReport(
            run_id=self.events.run_id,
            source_dialect=source_dialect,
            original_sql=sql,
            mapping=mapping,
            transpilation=transpilation,
            validation=validation,
            optimization=optimization,
            judge=judge,
            diff=diff,
            confidence_score=score,
            confidence_basis=basis,
            risk_level=risk,
            repair_attempts=repair_attempts,
            trace=self.trace,
            events=self.events.events,
        )

    def _validate(
        self,
        original_sql: str,
        mapping: MappingResult,
        transpilation: TranspileResult,
        live_schema: bool,
        *,
        attempt: int = 1,
    ) -> ValidationResult:
        static_issues = static_validate_postgres(transpilation.sql)
        result = self._invoke(
            "validator",
            self.validator,
            _payload(
                task="Validate the proposed PostgreSQL query.",
                original_sql=original_sql,
                mapping=mapping.model_dump(),
                proposed_sql=transpilation.sql,
                transformations=transpilation.transformations,
                assumptions=transpilation.assumptions,
                deterministic_parser_issues=[
                    issue.model_dump() for issue in static_issues
                ],
                live_schema_tools_available=live_schema,
            ),
            ValidationResult,
            attempt=attempt,
        )
        seen = {(issue.code, issue.message) for issue in result.issues}
        for issue in static_issues:
            if (issue.code, issue.message) not in seen:
                result.issues.append(issue)
        if "sqlglot PostgreSQL parse and read-only AST check" not in result.checks_run:
            result.checks_run.append("sqlglot PostgreSQL parse and read-only AST check")
        if any(issue.severity == "error" for issue in result.issues):
            result.valid = False
        return result


def build_diff(before: str, after: str) -> DiffSummary:
    lines = list(
        difflib.unified_diff(
            before.splitlines(),
            after.splitlines(),
            fromfile="legacy.sql",
            tofile="modernized.sql",
            lineterm="",
        )
    )
    additions = sum(
        1 for line in lines if line.startswith("+") and not line.startswith("+++")
    )
    deletions = sum(
        1 for line in lines if line.startswith("-") and not line.startswith("---")
    )
    return DiffSummary(
        additions=additions,
        deletions=deletions,
        unified_diff="\n".join(lines),
    )


def _stage_summary(output: BaseModel) -> str:
    if isinstance(output, MappingResult):
        return (
            f"Mapped {len(output.mappings)} entities; "
            f"{len(output.unresolved_entities)} unresolved"
        )
    if isinstance(output, TranspileResult):
        return f"Recorded {len(output.transformations)} dialect transformations"
    if isinstance(output, ValidationResult):
        return f"valid={output.valid}; {len(output.issues)} issue(s)"
    if isinstance(output, OptimizationResult):
        return (
            f"Recorded {len(output.changes)} rewrites and "
            f"{len(output.index_recommendations)} index proposal(s)"
        )
    if isinstance(output, JudgeAssessment):
        return f"verdict={output.verdict}; {len(output.semantic_risks)} risk(s)"
    return f"Produced {type(output).__name__}"


def confidence_score(
    mapping: MappingResult,
    validation: ValidationResult,
    judge: JudgeAssessment,
    repair_attempts: int,
    live_schema: bool,
) -> tuple[int, str]:
    """Compute a transparent heuristic; this is not an equivalence proof."""

    score = 92 if live_schema else 82
    score -= min(30, 10 * len(mapping.unresolved_entities))
    score -= min(20, 5 * repair_attempts)
    score -= 12 * sum(issue.severity == "error" for issue in validation.issues)
    score -= 3 * sum(issue.severity == "warning" for issue in validation.issues)
    score -= min(20, 4 * len(judge.semantic_risks))
    if judge.verdict == "review":
        score -= 5
    elif judge.verdict == "reject":
        score -= 20
    score = max(0, min(99, score))
    basis = (
        "Heuristic score from catalog grounding, unresolved mappings, validator "
        "issues, repair attempts, and judge risks. It is not a mathematical proof; "
        "run the source and target verification probes on equivalent snapshots."
    )
    return score, basis


def risk_level(
    score: int,
    mapping: MappingResult,
    validation: ValidationResult,
    judge: JudgeAssessment,
) -> str:
    if (
        score >= 85
        and not mapping.unresolved_entities
        and judge.verdict == "accept"
        and not any(issue.severity == "error" for issue in validation.issues)
    ):
        return "low"
    if score >= 65 and judge.verdict != "reject":
        return "medium"
    return "high"


def run_modernization(
    sql: str,
    config: AgentConfig,
    *,
    source_dialect: str,
    target_schema: str | None = None,
    live_schema: bool = False,
    event_sink: EventSink | None = None,
) -> ModernizationReport:
    """SDK entry point for one bounded modernization workflow."""

    cancel_signal = Event()
    timer = Timer(config.timeout_seconds, cancel_signal.set)
    timer.daemon = True
    timer.start()
    client = build_mcp_client(config, MODERNIZER_TOOLS) if live_schema else None
    context = client if client is not None else nullcontext()
    try:
        with context:
            role_tools: dict[str, list[Any]] = {}
            if client is not None:
                available = {tool.tool_name: tool for tool in client.list_tools_sync()}
                role_tools = {
                    "mapper": [
                        available[name]
                        for name in ("schema_catalog", "schema_of")
                        if name in available
                    ],
                    "validator": [
                        available[name]
                        for name in ("schema_of", "explain_plan")
                        if name in available
                    ],
                    "optimizer": [
                        available[name]
                        for name in ("schema_of", "explain_plan")
                        if name in available
                    ],
                }
            return Modernizer(
                config,
                role_tools=role_tools,
                cancel_signal=cancel_signal,
                event_sink=event_sink,
            ).run(
                sql,
                source_dialect=source_dialect,
                target_schema=target_schema,
                live_schema=live_schema,
            )
    finally:
        timer.cancel()
