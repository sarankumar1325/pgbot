from queryshift_agent.config import AgentConfig
from queryshift_agent.runtime import (
    MODEL_CREDENTIAL_ENV,
    build_model,
    queryshift_child_env,
)


def test_tool_process_does_not_receive_bedrock_credentials(monkeypatch):
    for name in MODEL_CREDENTIAL_ENV:
        monkeypatch.setenv(name, "secret")
    monkeypatch.setenv("DATABASE_URL", "postgres://trusted-db")

    child_env = queryshift_child_env()

    assert child_env["DATABASE_URL"] == "postgres://trusted-db"
    assert child_env["QUERYSHIFT_MCP_HIDE_CONNECTION_STRING"] == "1"
    for name in MODEL_CREDENTIAL_ENV:
        assert name not in child_env


def test_bedrock_model_uses_explicit_output_cap_and_adaptive_retries():
    model = build_model(
        AgentConfig(max_tokens_per_turn=1234, sdk_max_attempts=4, timeout_seconds=99)
    )

    assert model.config["max_tokens"] == 1234
    assert model.client.meta.config.retries["mode"] == "adaptive"
    assert model.client.meta.config.retries["total_max_attempts"] == 5
    assert model.client.meta.config.read_timeout == 99
