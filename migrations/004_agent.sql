-- Phase 6: agent evaluation runs — durable execution state + trace.

CREATE TABLE agent_runs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    submission_id   INTEGER NOT NULL REFERENCES submissions(id) ON DELETE CASCADE,
    faculty_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    goal            TEXT    NOT NULL DEFAULT '',
    kind            TEXT    NOT NULL DEFAULT 'EVALUATE'
                    CHECK (kind IN ('EVALUATE','RECONSIDER','FOLLOWUP')),
    status          TEXT    NOT NULL DEFAULT 'PENDING'
                    CHECK (status IN ('PENDING','RUNNING','DONE','FAILED')),
    iterations      INTEGER NOT NULL DEFAULT 0,
    max_iterations  INTEGER NOT NULL DEFAULT 12,
    messages        TEXT    NOT NULL DEFAULT '[]',   -- full chat transcript (JSON)
    draft           TEXT    NOT NULL DEFAULT '{}',   -- accumulated evaluation draft (JSON)
    error           TEXT    NOT NULL DEFAULT '',
    result_evaluation_id INTEGER REFERENCES evaluations(id) ON DELETE SET NULL,
    followup_readonly INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_runs_submission ON agent_runs(submission_id);
CREATE INDEX IF NOT EXISTS idx_agent_runs_status ON agent_runs(status, id);

CREATE TABLE agent_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id     INTEGER NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('MODEL_CALL','TOOL_CALL','TOOL_RESULT','TOOL_ERROR','FINAL','ERROR','USER_MSG','NOTE')),
    payload    TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_events_run ON agent_events(run_id, seq);
