from queryshift_agent.prompt import SYSTEM_PROMPT


def test_prompt_keeps_model_and_database_provider_separate():
    assert "Bedrock is only your model provider" in SYSTEM_PROMPT
    assert "Never assume the database is on AWS" in SYSTEM_PROMPT
    assert "Neon" in SYSTEM_PROMPT


def test_prompt_preserves_queryshift_safety_invariants():
    assert "Never write to the database" in SYSTEM_PROMPT
    assert "EXPLAIN ANALYZE" in SYSTEM_PROMPT
    assert "Never invent" in SYSTEM_PROMPT
    assert "connection string" in SYSTEM_PROMPT
