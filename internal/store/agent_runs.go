package store

import "time"

// AgentRun is sanitized execution metadata. It intentionally has no fields for
// SQL, prompts, connection information, model output, or error messages.
type AgentRun struct {
	RunID           string
	StartedAt       time.Time
	CompletedAt     time.Time
	SourceDialect   string
	TargetDialect   string
	ModelID         string
	LiveSchema      bool
	Status          string
	ConfidenceScore *int
	RiskLevel       string
	EventsJSON      string
}

func (s *Store) SaveAgentRun(run AgentRun) error {
	_, err := s.db.Exec(`
		INSERT INTO agent_runs
		  (run_id, started_at, completed_at, source_dialect, target_dialect,
		   model_id, live_schema, status, confidence_score, risk_level, events_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id) DO UPDATE SET
		  completed_at=excluded.completed_at,
		  status=excluded.status,
		  confidence_score=excluded.confidence_score,
		  risk_level=excluded.risk_level,
		  events_json=excluded.events_json`,
		run.RunID,
		run.StartedAt.UTC().Unix(),
		run.CompletedAt.UTC().Unix(),
		run.SourceDialect,
		run.TargetDialect,
		run.ModelID,
		boolToInt(run.LiveSchema),
		run.Status,
		run.ConfidenceScore,
		run.RiskLevel,
		run.EventsJSON,
	)
	return err
}

func (s *Store) RecentAgentRuns(limit int) ([]AgentRun, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT run_id, started_at, completed_at, source_dialect, target_dialect,
		       model_id, live_schema, status, confidence_score, risk_level, events_json
		FROM agent_runs ORDER BY completed_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []AgentRun
	for rows.Next() {
		var run AgentRun
		var started, completed int64
		var live int
		if err := rows.Scan(
			&run.RunID, &started, &completed, &run.SourceDialect, &run.TargetDialect,
			&run.ModelID, &live, &run.Status, &run.ConfidenceScore, &run.RiskLevel,
			&run.EventsJSON,
		); err != nil {
			return nil, err
		}
		run.StartedAt = time.Unix(started, 0).UTC()
		run.CompletedAt = time.Unix(completed, 0).UTC()
		run.LiveSchema = live != 0
		runs = append(runs, run)
	}
	return runs, rows.Err()
}
