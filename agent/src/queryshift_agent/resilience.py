"""Provider failure classification and bounded, jittered retry timing."""

from __future__ import annotations

import random
from dataclasses import dataclass
from typing import Callable


@dataclass(frozen=True)
class FailureInfo:
    category: str
    retryable: bool


_RETRYABLE_TYPES = {
    "ModelThrottledException": "rate_limit",
    "ThrottlingException": "rate_limit",
    "ServiceUnavailableException": "provider_unavailable",
    "InternalServerException": "provider_internal",
    "ModelTimeoutException": "provider_timeout",
    "ReadTimeoutError": "transport_timeout",
    "ConnectTimeoutError": "transport_timeout",
    "ConnectionError": "transport",
    "TimeoutError": "transport_timeout",
}

_NON_RETRYABLE_TYPES = {
    "AccessDeniedException": "authentication",
    "UnrecognizedClientException": "authentication",
    "ValidationException": "invalid_request",
    "ResourceNotFoundException": "not_found",
    "ContextWindowOverflowException": "context_overflow",
    "StructuredOutputException": "invalid_model_output",
}

_RETRYABLE_TEXT = (
    "throttlingexception",
    "too many requests",
    "service unavailable",
    "temporarily unavailable",
    "internalserverexception",
    "model time",
    "connection reset",
    "connection aborted",
)

_NON_RETRYABLE_TEXT = (
    "accessdeniedexception",
    "validationexception",
    "resource not found",
    "context window",
    "maximum context",
    "content filtered",
)


def _exception_chain(exc: BaseException) -> list[BaseException]:
    chain: list[BaseException] = []
    seen: set[int] = set()
    current: BaseException | None = exc
    while current is not None and id(current) not in seen and len(chain) < 12:
        seen.add(id(current))
        chain.append(current)
        current = current.__cause__ or current.__context__
    return chain


def classify_failure(exc: BaseException) -> FailureInfo:
    """Classify only positively identified transient failures as retryable."""

    chain = _exception_chain(exc)
    for item in chain:
        name = type(item).__name__
        if name in _NON_RETRYABLE_TYPES:
            return FailureInfo(_NON_RETRYABLE_TYPES[name], False)
        if name in _RETRYABLE_TYPES:
            return FailureInfo(_RETRYABLE_TYPES[name], True)

    text = "\n".join(str(item) for item in chain).lower()
    if any(marker in text for marker in _NON_RETRYABLE_TEXT):
        return FailureInfo("deterministic_provider_error", False)
    if any(marker in text for marker in _RETRYABLE_TEXT):
        return FailureInfo("transient_provider_error", True)
    return FailureInfo("unknown", False)


def retry_delay(
    retry_number: int,
    *,
    base_seconds: float,
    max_seconds: float,
    random_value: Callable[[], float] = random.random,
) -> float:
    """Exponential backoff with bounded ±25% jitter."""

    raw = min(max_seconds, base_seconds * (2 ** max(0, retry_number - 1)))
    jitter = 0.75 + (0.5 * random_value())
    return min(max_seconds, raw * jitter)
