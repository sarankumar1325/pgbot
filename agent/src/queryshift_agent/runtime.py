"""Strands runtime that connects Bedrock to queryshift's existing MCP server."""

from __future__ import annotations

import os
from threading import Event, Timer
from typing import Any

from botocore.config import Config as BotoConfig
from mcp import StdioServerParameters, stdio_client
from strands import Agent
from strands.models import BedrockModel
from strands.tools.mcp import MCPClient

from .config import READ_ONLY_TOOLS, AgentConfig
from .prompt import SYSTEM_PROMPT

MODEL_CREDENTIAL_ENV = (
    "AWS_ACCESS_KEY_ID",
    "AWS_SECRET_ACCESS_KEY",
    "AWS_SESSION_TOKEN",
    "AWS_SECURITY_TOKEN",
    "AWS_BEARER_TOKEN_BEDROCK",
    "AWS_WEB_IDENTITY_TOKEN_FILE",
    "AWS_ROLE_ARN",
)


def queryshift_child_env() -> dict[str, str]:
    """Return tool-process configuration without Bedrock credentials."""

    child_env = dict(os.environ)
    for name in MODEL_CREDENTIAL_ENV:
        child_env.pop(name, None)
    child_env["QUERYSHIFT_MCP_HIDE_CONNECTION_STRING"] = "1"
    return child_env


def build_mcp_client(
    config: AgentConfig, allowed_tools: tuple[str, ...] = READ_ONLY_TOOLS
) -> MCPClient:
    """Start queryshift's stdio MCP server with credentials kept out of model input."""

    # The child needs DATABASE_URL/PGSERVICE and normal process configuration.
    # This environment is supplied to the process by the host application; it is
    # never included in the prompt or in a model-generated tool argument.
    child_env = queryshift_child_env()
    return MCPClient(
        lambda: stdio_client(
            StdioServerParameters(
                command=config.queryshift_bin,
                args=["mcp"],
                env=child_env,
            )
        ),
        tool_filters={"allowed": list(allowed_tools)},
    )


def build_model(config: AgentConfig) -> BedrockModel:
    """Create the bounded Bedrock model shared by one agent workflow."""

    return BedrockModel(
        model_id=config.model_id,
        region_name=config.region,
        max_tokens=config.max_tokens_per_turn,
        api_key=os.getenv("AWS_BEARER_TOKEN_BEDROCK") or None,
        boto_client_config=BotoConfig(
            retries={"max_attempts": config.sdk_max_attempts, "mode": "adaptive"},
            connect_timeout=10,
            read_timeout=config.timeout_seconds,
        ),
    )


def build_agent(config: AgentConfig, mcp_client: MCPClient) -> Agent:
    """Create a single diagnostic agent; Bedrock is only the model provider."""

    return Agent(
        name="queryshift-diagnostics",
        description="Read-only, evidence-grounded PostgreSQL diagnostics",
        model=build_model(config),
        system_prompt=SYSTEM_PROMPT,
        tools=[mcp_client],
    )


def run(question: str, config: AgentConfig) -> Any:
    """Run one bounded reason/tool/observe loop and return the Strands result."""

    mcp_client = build_mcp_client(config)
    cancel_signal = Event()
    timer = Timer(config.timeout_seconds, cancel_signal.set)
    timer.daemon = True
    timer.start()
    try:
        with mcp_client:
            agent = build_agent(config, mcp_client)
            return agent(
                question,
                limits={
                    "turns": config.max_turns,
                    "total_tokens": config.max_total_tokens,
                    "output_tokens": config.max_output_tokens,
                },
                cancel_signal=cancel_signal,
            )
    finally:
        timer.cancel()
