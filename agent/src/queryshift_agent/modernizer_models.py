"""Structured contracts for the query-modernization pipeline."""

from __future__ import annotations

from datetime import UTC, datetime
from typing import Literal

from pydantic import BaseModel, Field

from .events import RunEvent


class EntityMapping(BaseModel):
    source_entity: str
    target_entity: str | None = None
    confidence: int = Field(ge=0, le=100)
    evidence: str


class MappingResult(BaseModel):
    detected_dialect: str
    mappings: list[EntityMapping] = Field(default_factory=list)
    unresolved_entities: list[str] = Field(default_factory=list)
    notes: list[str] = Field(default_factory=list)


class TranspileResult(BaseModel):
    sql: str
    transformations: list[str] = Field(default_factory=list)
    assumptions: list[str] = Field(default_factory=list)


class ValidationIssue(BaseModel):
    severity: Literal["error", "warning", "info"]
    code: str
    message: str
    location: str | None = None


class ValidationResult(BaseModel):
    valid: bool
    issues: list[ValidationIssue] = Field(default_factory=list)
    retry_instructions: list[str] = Field(default_factory=list)
    checks_run: list[str] = Field(default_factory=list)


class IndexRecommendation(BaseModel):
    name: str
    table: str
    columns: list[str]
    ddl: str
    rationale: str
    evidence: str
    planner_validated: bool = False
    estimated_impact: str = "unverified"


class OptimizationResult(BaseModel):
    sql: str
    changes: list[str] = Field(default_factory=list)
    index_recommendations: list[IndexRecommendation] = Field(default_factory=list)
    cautions: list[str] = Field(default_factory=list)


class VerificationProbe(BaseModel):
    source_sql: str
    target_sql: str
    comparison_method: str
    limitations: list[str] = Field(default_factory=list)


class JudgeAssessment(BaseModel):
    verdict: Literal["accept", "review", "reject"]
    semantic_risks: list[str] = Field(default_factory=list)
    preserved_behaviors: list[str] = Field(default_factory=list)
    verification_probe: VerificationProbe


class DiffSummary(BaseModel):
    additions: int
    deletions: int
    unified_diff: str


class TraceEvent(BaseModel):
    stage: str
    attempt: int = 1
    transport_attempt: int = Field(default=1, ge=1)
    agent: str | None = None
    status: Literal["started", "completed", "failed", "retry", "fallback"]
    duration_ms: int = Field(ge=0)
    summary: str
    stop_reason: str | None = None
    input_tokens: int = Field(default=0, ge=0)
    output_tokens: int = Field(default=0, ge=0)
    total_tokens: int = Field(default=0, ge=0)
    cache_read_tokens: int = Field(default=0, ge=0)
    cache_write_tokens: int = Field(default=0, ge=0)
    tool_calls: int = Field(default=0, ge=0)
    error_category: str | None = None
    retryable: bool | None = None


class ModernizationReport(BaseModel):
    run_id: str
    source_dialect: str
    target_dialect: str = "postgres"
    original_sql: str
    mapping: MappingResult
    transpilation: TranspileResult
    validation: ValidationResult
    optimization: OptimizationResult
    judge: JudgeAssessment
    diff: DiffSummary
    confidence_score: int = Field(ge=0, le=100)
    confidence_basis: str
    risk_level: Literal["low", "medium", "high"]
    repair_attempts: int = Field(ge=0)
    trace: list[TraceEvent] = Field(default_factory=list)
    events: list[RunEvent] = Field(default_factory=list)
    generated_at: datetime = Field(default_factory=lambda: datetime.now(UTC))
