"""Typed run events used by CLIs today and streaming UIs in a later slice."""

from __future__ import annotations

from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any, Literal
from uuid import uuid4

from pydantic import BaseModel, Field

EventKind = Literal[
    "run_started",
    "stage_started",
    "retry_scheduled",
    "stage_completed",
    "stage_failed",
    "fallback_applied",
    "run_completed",
    "run_failed",
]


class RunEvent(BaseModel):
    run_id: str
    sequence: int = Field(ge=1)
    kind: EventKind
    stage: str | None = None
    logical_attempt: int = Field(default=1, ge=1)
    transport_attempt: int = Field(default=1, ge=1)
    timestamp: datetime = Field(default_factory=lambda: datetime.now(UTC))
    data: dict[str, Any] = Field(default_factory=dict)


EventSink = Callable[[RunEvent], None]


class EventRecorder:
    """Append-only in-memory event log with an optional streaming sink."""

    def __init__(self, sink: EventSink | None = None, run_id: str | None = None) -> None:
        self.run_id = run_id or uuid4().hex
        self.events: list[RunEvent] = []
        self._sink = sink

    def emit(
        self,
        kind: EventKind,
        *,
        stage: str | None = None,
        logical_attempt: int = 1,
        transport_attempt: int = 1,
        **data: Any,
    ) -> RunEvent:
        event = RunEvent(
            run_id=self.run_id,
            sequence=len(self.events) + 1,
            kind=kind,
            stage=stage,
            logical_attempt=logical_attempt,
            transport_attempt=transport_attempt,
            data=data,
        )
        self.events.append(event)
        if self._sink is not None:
            self._sink(event)
        return event
