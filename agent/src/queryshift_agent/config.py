"""Configuration that stays independent from Strands for cheap validation/tests."""

from __future__ import annotations

import os
from dataclasses import dataclass

from dotenv import find_dotenv, load_dotenv

DEFAULT_MODEL = "deepseek.v3.2"

# record_index_verdict intentionally stays out: it mutates queryshift's local store.
# The agent's first release is read-only from both the database and local-state
# perspectives.
READ_ONLY_TOOLS = (
    "inspect",
    "unused_indexes",
    "top_queries",
    "vacuum_health",
    "suggest_indexes",
    "explain_plan",
    "compare_to_baseline",
    "why",
    "schema_catalog",
    "schema_of",
    "explain_finding",
    "index_code_correlation",
)

MODERNIZER_TOOLS = (
    "schema_catalog",
    "schema_of",
    "explain_plan",
)


@dataclass(frozen=True)
class AgentConfig:
    """Runtime limits and Bedrock settings for one agent invocation."""

    model_id: str = DEFAULT_MODEL
    region: str = "us-east-1"
    queryshift_bin: str = "queryshift"
    max_turns: int = 8
    max_tokens_per_turn: int = 4096
    max_total_tokens: int = 30_000
    max_output_tokens: int = 8_000
    timeout_seconds: int = 180
    max_repair_attempts: int = 2
    max_sql_chars: int = 100_000
    sdk_max_attempts: int = 5
    max_transient_retries: int = 2
    retry_base_seconds: float = 0.5
    retry_max_seconds: float = 8.0

    @classmethod
    def from_env(cls) -> AgentConfig:
        # Local development convenience. Explicit shell variables retain precedence,
        # and .env is git-ignored so credentials never belong in source control.
        dotenv_path = find_dotenv(usecwd=True)
        if dotenv_path:
            load_dotenv(dotenv_path, override=False)
        return cls(
            model_id=os.getenv("QUERYSHIFT_AGENT_MODEL", DEFAULT_MODEL),
            region=os.getenv(
                "AWS_REGION", os.getenv("AWS_DEFAULT_REGION", "us-east-1")
            ),
            queryshift_bin=os.getenv("QUERYSHIFT_BIN", "queryshift"),
            max_turns=_positive_int("QUERYSHIFT_AGENT_MAX_TURNS", 8),
            max_tokens_per_turn=_positive_int(
                "QUERYSHIFT_AGENT_MAX_TOKENS_PER_TURN", 4096
            ),
            max_total_tokens=_positive_int("QUERYSHIFT_AGENT_MAX_TOTAL_TOKENS", 30_000),
            max_output_tokens=_positive_int(
                "QUERYSHIFT_AGENT_MAX_OUTPUT_TOKENS", 8_000
            ),
            timeout_seconds=_positive_int("QUERYSHIFT_AGENT_TIMEOUT_SECONDS", 180),
            max_repair_attempts=_non_negative_int(
                "QUERYSHIFT_MODERNIZER_MAX_REPAIRS", 2
            ),
            max_sql_chars=_positive_int("QUERYSHIFT_MODERNIZER_MAX_SQL_CHARS", 100_000),
            sdk_max_attempts=_positive_int("QUERYSHIFT_BEDROCK_SDK_MAX_ATTEMPTS", 5),
            max_transient_retries=_non_negative_int(
                "QUERYSHIFT_AGENT_MAX_TRANSIENT_RETRIES", 2
            ),
            retry_base_seconds=_positive_float(
                "QUERYSHIFT_AGENT_RETRY_BASE_SECONDS", 0.5
            ),
            retry_max_seconds=_positive_float(
                "QUERYSHIFT_AGENT_RETRY_MAX_SECONDS", 8.0
            ),
        )


def _positive_int(name: str, default: int) -> int:
    raw = os.getenv(name)
    if raw is None or not raw.strip():
        return default
    try:
        value = int(raw)
    except ValueError as exc:
        raise ValueError(f"{name} must be a positive integer, got {raw!r}") from exc
    if value <= 0:
        raise ValueError(f"{name} must be a positive integer, got {raw!r}")
    return value


def _non_negative_int(name: str, default: int) -> int:
    raw = os.getenv(name)
    if raw is None or not raw.strip():
        return default
    try:
        value = int(raw)
    except ValueError as exc:
        raise ValueError(f"{name} must be a non-negative integer, got {raw!r}") from exc
    if value < 0:
        raise ValueError(f"{name} must be a non-negative integer, got {raw!r}")
    return value


def _positive_float(name: str, default: float) -> float:
    raw = os.getenv(name)
    if raw is None or not raw.strip():
        return default
    try:
        value = float(raw)
    except ValueError as exc:
        raise ValueError(f"{name} must be a positive number, got {raw!r}") from exc
    if value <= 0:
        raise ValueError(f"{name} must be a positive number, got {raw!r}")
    return value
