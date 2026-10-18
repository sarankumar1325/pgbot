-- Sanitized agent execution history. SQL, prompts, credentials, connection
-- strings, model reasoning, and provider error messages are never stored here.
CREATE TABLE IF NOT EXISTS agent_runs (
    run_id            TEXT PRIMARY KEY,
    started_at        INTEGER NOT NULL,
    completed_at      INTEGER NOT NULL,
    source_dialect    TEXT NOT NULL,
    target_dialect    TEXT NOT NULL,
    model_id          TEXT NOT NULL,
    live_schema       INTEGER NOT NULL,
    status            TEXT NOT NULL,
    confidence_score  INTEGER,
    risk_level        TEXT NOT NULL,
    events_json       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS agent_runs_completed_at
ON agent_runs(completed_at DESC);
