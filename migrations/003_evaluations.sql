-- Phase 4: evaluation engine persistence (additive only).

-- Evaluation status on the submission (distinct from the submission status).
ALTER TABLE submissions ADD COLUMN eval_status TEXT NOT NULL DEFAULT 'NOT_REQUESTED'
    CHECK (eval_status IN ('NOT_REQUESTED','PENDING','RUNNING','NEEDS_REVIEW','APPROVED','FAILED'));
CREATE INDEX IF NOT EXISTS idx_submissions_eval_status ON submissions(eval_status);

-- Durable evaluation queue (single worker, survives restarts).
CREATE TABLE evaluation_jobs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    submission_id  INTEGER NOT NULL REFERENCES submissions(id) ON DELETE CASCADE,
    status         TEXT NOT NULL DEFAULT 'PENDING'
                   CHECK (status IN ('PENDING','RUNNING','COMPLETED','FAILED','SUPERSEDED')),
    tries          INTEGER NOT NULL DEFAULT 0,
    error          TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_jobs_pending ON evaluation_jobs(status, id);

-- Extra engine metadata on evaluation attempts (additive columns).
ALTER TABLE evaluations ADD COLUMN confidence_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE evaluations ADD COLUMN warnings TEXT NOT NULL DEFAULT '[]';   -- JSON array of validation warnings
ALTER TABLE evaluations ADD COLUMN extraction_ok INTEGER NOT NULL DEFAULT 1;
ALTER TABLE evaluations ADD COLUMN approved_by INTEGER REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE evaluations ADD COLUMN approved_at TEXT NOT NULL DEFAULT '';

-- A criterion result's evidence is stored as a JSON array of {quote, explanation}.
ALTER TABLE criterion_results ADD COLUMN evidence_problems INTEGER NOT NULL DEFAULT 0;

-- Faculty overrides are additive facts (never overwrite model output).
CREATE TABLE criteria_overrides (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    evaluation_id  INTEGER NOT NULL REFERENCES evaluations(id) ON DELETE CASCADE,
    criterion_id   INTEGER NOT NULL REFERENCES rubric_criteria(id) ON DELETE CASCADE,
    awarded_points REAL NOT NULL CHECK (awarded_points >= 0),
    faculty_feedback TEXT NOT NULL DEFAULT '',
    faculty_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at     TEXT NOT NULL,
    UNIQUE (evaluation_id, criterion_id)
);
