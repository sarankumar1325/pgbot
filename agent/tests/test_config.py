from argparse import Namespace

import pytest
from queryshift_agent.cli import resolved_config
from queryshift_agent.config import DEFAULT_MODEL, READ_ONLY_TOOLS, AgentConfig


def args(**overrides):
    values = {
        "model": None,
        "region": None,
        "queryshift_bin": None,
        "max_turns": None,
        "timeout": None,
    }
    values.update(overrides)
    return Namespace(**values)


def test_defaults_are_bedrock_and_bounded(monkeypatch):
    for name in (
        "QUERYSHIFT_AGENT_MODEL",
        "AWS_REGION",
        "AWS_DEFAULT_REGION",
        "QUERYSHIFT_BIN",
        "QUERYSHIFT_AGENT_MAX_TURNS",
        "QUERYSHIFT_AGENT_MAX_TOKENS_PER_TURN",
        "QUERYSHIFT_AGENT_MAX_TOTAL_TOKENS",
        "QUERYSHIFT_AGENT_MAX_OUTPUT_TOKENS",
        "QUERYSHIFT_AGENT_TIMEOUT_SECONDS",
        "QUERYSHIFT_MODERNIZER_MAX_REPAIRS",
        "QUERYSHIFT_MODERNIZER_MAX_SQL_CHARS",
        "QUERYSHIFT_BEDROCK_SDK_MAX_ATTEMPTS",
        "QUERYSHIFT_AGENT_MAX_TRANSIENT_RETRIES",
        "QUERYSHIFT_AGENT_RETRY_BASE_SECONDS",
        "QUERYSHIFT_AGENT_RETRY_MAX_SECONDS",
    ):
        monkeypatch.delenv(name, raising=False)

    config = AgentConfig.from_env()
    assert config.model_id == DEFAULT_MODEL
    assert config.region == "us-east-1"
    assert config.max_turns == 8
    assert config.max_tokens_per_turn == 4096
    assert config.timeout_seconds == 180
    assert config.max_repair_attempts == 2
    assert config.max_sql_chars == 100_000
    assert config.sdk_max_attempts == 5
    assert config.max_transient_retries == 2
    assert config.retry_base_seconds == 0.5
    assert config.retry_max_seconds == 8.0


def test_loads_dotenv_without_overwriting_shell(monkeypatch, tmp_path):
    monkeypatch.chdir(tmp_path)
    monkeypatch.delenv("QUERYSHIFT_AGENT_MODEL", raising=False)
    monkeypatch.delenv("AWS_REGION", raising=False)
    (tmp_path / ".env").write_text(
        "QUERYSHIFT_AGENT_MODEL=dotenv-model\nAWS_REGION=ap-south-1\n",
        encoding="utf-8",
    )

    config = AgentConfig.from_env()

    assert config.model_id == "dotenv-model"
    assert config.region == "ap-south-1"

    monkeypatch.setenv("QUERYSHIFT_AGENT_MODEL", "shell-model")
    assert AgentConfig.from_env().model_id == "shell-model"


def test_cli_overrides_environment(monkeypatch):
    monkeypatch.setenv("QUERYSHIFT_AGENT_MODEL", "env-model")
    monkeypatch.setenv("AWS_REGION", "us-east-1")
    config = resolved_config(args(model="cli-model", region="ap-south-1", max_turns=4))
    assert config.model_id == "cli-model"
    assert config.region == "ap-south-1"
    assert config.max_turns == 4


@pytest.mark.parametrize("value", ["0", "-1", "not-a-number"])
def test_environment_limits_must_be_positive(monkeypatch, value):
    monkeypatch.setenv("QUERYSHIFT_AGENT_MAX_TURNS", value)
    with pytest.raises(ValueError, match="positive integer"):
        AgentConfig.from_env()


def test_local_write_tool_is_not_available_to_model():
    assert "record_index_verdict" not in READ_ONLY_TOOLS
    assert "inspect" in READ_ONLY_TOOLS
    assert "suggest_indexes" in READ_ONLY_TOOLS
    assert "schema_catalog" in READ_ONLY_TOOLS


def test_cli_limits_must_be_positive():
    with pytest.raises(ValueError, match="--max-turns"):
        resolved_config(args(max_turns=0))
    with pytest.raises(ValueError, match="--timeout"):
        resolved_config(args(timeout=0))
