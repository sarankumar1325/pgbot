from types import SimpleNamespace

import pytest

from queryshift_agent.config import AgentConfig
from queryshift_agent.events import EventRecorder
from queryshift_agent.modernizer import (
    Modernizer,
    build_diff,
    confidence_score,
    risk_level,
    static_validate_postgres,
)
from queryshift_agent.modernizer_models import (
    JudgeAssessment,
    MappingResult,
    ValidationIssue,
    ValidationResult,
    VerificationProbe,
)


class ModelThrottledException(Exception):
    pass


class FakeAgent:
    name = "fake-mapper"

    def __init__(self):
        self.calls = 0

    def __call__(self, *args, **kwargs):
        self.calls += 1
        if self.calls == 1:
            raise ModelThrottledException("throttled")
        return SimpleNamespace(
            structured_output=MappingResult(detected_dialect="oracle"),
            stop_reason="end_turn",
            metrics=SimpleNamespace(
                accumulated_usage={
                    "inputTokens": 20,
                    "outputTokens": 10,
                    "totalTokens": 30,
                },
                tool_metrics={},
            ),
        )


def judge(verdict="accept", risks=None):
    return JudgeAssessment(
        verdict=verdict,
        semantic_risks=risks or [],
        verification_probe=VerificationProbe(
            source_sql="SELECT 1",
            target_sql="SELECT 1",
            comparison_method="compare",
        ),
    )


def test_static_validation_accepts_one_read_only_query():
    assert static_validate_postgres("SELECT id FROM public.orders") == []


def test_static_validation_rejects_writes_and_multiple_statements():
    write_codes = {
        issue.code for issue in static_validate_postgres("DELETE FROM orders")
    }
    multi_codes = {
        issue.code for issue in static_validate_postgres("SELECT 1; SELECT 2")
    }
    assert "write_statement" in write_codes
    assert "multiple_statements" in multi_codes


def test_static_validation_flags_select_star():
    issues = static_validate_postgres("SELECT * FROM orders")
    assert [(issue.severity, issue.code) for issue in issues] == [
        ("warning", "select_star")
    ]


def test_diff_counts_changed_lines_without_headers():
    diff = build_diff("SELECT old\nFROM t", "SELECT new\nFROM t")
    assert diff.additions == 1
    assert diff.deletions == 1
    assert "--- legacy.sql" in diff.unified_diff


def test_confidence_is_lower_without_catalog_and_with_risks():
    mapping = MappingResult(
        detected_dialect="oracle", unresolved_entities=["TBL_UNKNOWN"]
    )
    validation = ValidationResult(
        valid=True,
        issues=[ValidationIssue(severity="warning", code="null", message="review")],
    )
    score, basis = confidence_score(
        mapping, validation, judge("review", ["NULL behavior"]), 1, False
    )
    assert score == 55
    assert "not a mathematical proof" in basis
    assert risk_level(score, mapping, validation, judge("review")) == "high"


def test_low_risk_requires_live_catalog_and_clean_acceptance():
    mapping = MappingResult(detected_dialect="tsql")
    validation = ValidationResult(valid=True)
    assessment = judge()
    score, _ = confidence_score(mapping, validation, assessment, 0, True)
    assert score == 92
    assert risk_level(score, mapping, validation, assessment) == "low"


def test_transient_stage_failure_is_retried_and_traced():
    modernizer = object.__new__(Modernizer)
    modernizer.config = AgentConfig(
        max_transient_retries=1,
        retry_base_seconds=0.01,
        retry_max_seconds=0.01,
    )
    modernizer.cancel_signal = SimpleNamespace(is_set=lambda: False)
    modernizer.events = EventRecorder(run_id="run-test")
    modernizer._sleep = lambda _: None
    modernizer.trace = []
    agent = FakeAgent()

    result = modernizer._invoke(
        "mapper", agent, "{}", MappingResult  # type: ignore[arg-type]
    )

    assert result.detected_dialect == "oracle"
    assert agent.calls == 2
    assert [event.status for event in modernizer.trace] == [
        "failed",
        "retry",
        "completed",
    ]
    assert modernizer.trace[-1].total_tokens == 30
    assert [event.kind for event in modernizer.events.events] == [
        "stage_started",
        "stage_failed",
        "retry_scheduled",
        "stage_started",
        "stage_completed",
    ]


def test_failed_run_emits_terminal_event():
    modernizer = object.__new__(Modernizer)
    modernizer.config = AgentConfig()
    modernizer.events = EventRecorder(run_id="run-failed")

    with pytest.raises(ValueError, match="legacy SQL is empty"):
        modernizer.run("", source_dialect="oracle")

    assert modernizer.events.events[-1].kind == "run_failed"
    assert modernizer.events.events[-1].data["error_type"] == "ValueError"
