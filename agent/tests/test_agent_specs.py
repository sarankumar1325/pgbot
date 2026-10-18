import pytest

from queryshift_agent.agent_specs import (
    AgentSpec,
    ApprovalRequired,
    ToolPermission,
)


def test_last_matching_permission_rule_wins():
    spec = AgentSpec(
        name="mapper",
        description="maps",
        system_prompt="map",
        permissions=(
            ToolPermission("*", "deny"),
            ToolPermission("schema_*", "allow"),
            ToolPermission("schema_write", "ask"),
        ),
    )

    assert spec.permission_for("inspect") == "deny"
    assert spec.permission_for("schema_of") == "allow"
    assert spec.permission_for("schema_write") == "ask"


def test_ask_gated_tool_is_never_silently_exposed():
    spec = AgentSpec(
        name="writer",
        description="writes",
        system_prompt="write",
        permissions=(ToolPermission("*", "ask"),),
    )
    with pytest.raises(ApprovalRequired, match="requires approval"):
        spec.resolve_tools({"apply_change": object()})


def test_denied_tools_are_filtered_from_agent_surface():
    allowed = object()
    spec = AgentSpec(
        name="reader",
        description="reads",
        system_prompt="read",
        permissions=(
            ToolPermission("*", "deny"),
            ToolPermission("schema_of", "allow"),
        ),
    )
    assert spec.resolve_tools({"schema_of": allowed, "apply_change": object()}) == [
        allowed
    ]
