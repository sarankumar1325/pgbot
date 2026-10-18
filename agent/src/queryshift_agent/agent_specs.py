"""Declarative agent profiles and OpenCode-style tool permission rules."""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass, field
from fnmatch import fnmatchcase
from typing import Any, Literal

PermissionEffect = Literal["allow", "ask", "deny"]


@dataclass(frozen=True)
class ToolPermission:
    """One ordered tool rule. The last matching rule wins."""

    pattern: str
    effect: PermissionEffect


class ApprovalRequired(RuntimeError):
    """Raised when a non-interactive agent attempts to expose an ask-gated tool."""


@dataclass(frozen=True)
class AgentSpec:
    """Configuration for one specialist, independent from the agent framework."""

    name: str
    description: str
    system_prompt: str
    mode: Literal["primary", "specialist"] = "specialist"
    permissions: tuple[ToolPermission, ...] = field(
        default_factory=lambda: (ToolPermission("*", "deny"),)
    )

    def permission_for(self, tool_name: str) -> PermissionEffect:
        effect: PermissionEffect = "ask"
        for rule in self.permissions:
            if fnmatchcase(tool_name, rule.pattern):
                effect = rule.effect
        return effect

    def resolve_tools(self, available: Mapping[str, Any]) -> list[Any]:
        """Return only allowed tools; ask-gated tools need an approval broker."""

        resolved: list[Any] = []
        for name, tool in available.items():
            effect = self.permission_for(name)
            if effect == "allow":
                resolved.append(tool)
            elif effect == "ask":
                raise ApprovalRequired(
                    f"agent {self.name!r} requires approval before tool {name!r} "
                    "can be exposed"
                )
        return resolved
