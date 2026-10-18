from queryshift_agent.resilience import classify_failure, retry_delay


class ModelThrottledException(Exception):
    pass


class ValidationException(Exception):
    pass


def test_failure_classification_retries_only_known_transients():
    transient = classify_failure(ModelThrottledException("slow down"))
    invalid = classify_failure(ValidationException("bad request"))
    unknown = classify_failure(RuntimeError("something odd"))

    assert (transient.category, transient.retryable) == ("rate_limit", True)
    assert invalid.retryable is False
    assert unknown.retryable is False


def test_retry_delay_is_exponential_jittered_and_capped():
    assert retry_delay(
        1, base_seconds=2, max_seconds=10, random_value=lambda: 0.5
    ) == 2
    assert retry_delay(
        3, base_seconds=2, max_seconds=10, random_value=lambda: 0.5
    ) == 8
    assert retry_delay(
        8, base_seconds=2, max_seconds=10, random_value=lambda: 0.5
    ) == 10
