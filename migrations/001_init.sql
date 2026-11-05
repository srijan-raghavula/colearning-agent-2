-- EDUCORE initial schema (Phase 2)
-- Executed by the migration runner inside a transaction.

CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    email         TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    name          TEXT    NOT NULL,
    password_hash TEXT    NOT NULL,
    role          TEXT    NOT NULL CHECK (role IN ('FACULTY', 'STUDENT')),
    created_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE courses (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    faculty_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT    NOT NULL,
    code        TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    description TEXT    NOT NULL DEFAULT '',
    created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE enrollments (
    student_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    course_id  INTEGER NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    enrolled_at TEXT   NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (student_id, course_id)
);

CREATE INDEX idx_enrollments_course ON enrollments(course_id);

CREATE TABLE assignments (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    course_id   INTEGER NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    title       TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    topic       TEXT    NOT NULL DEFAULT '',
    due_date    TEXT    NOT NULL,
    published   INTEGER NOT NULL DEFAULT 0 CHECK (published IN (0, 1)),
    total_marks INTEGER NOT NULL DEFAULT 100 CHECK (total_marks > 0),
    created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_assignments_course ON assignments(course_id);

CREATE TABLE rubric_criteria (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    assignment_id INTEGER NOT NULL REFERENCES assignments(id) ON DELETE CASCADE,
    position     INTEGER NOT NULL DEFAULT 1,
    title        TEXT    NOT NULL,
    description  TEXT    NOT NULL DEFAULT '',
    max_points   INTEGER NOT NULL CHECK (max_points >= 0),
    topic        TEXT    NOT NULL DEFAULT '',
    keywords     TEXT    NOT NULL DEFAULT '',
    created_at   TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_criteria_assignment ON rubric_criteria(assignment_id);

CREATE TABLE submissions (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    assignment_id  INTEGER NOT NULL REFERENCES assignments(id) ON DELETE CASCADE,
    student_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content        TEXT    NOT NULL DEFAULT '',
    filename       TEXT,
    file_path      TEXT,
    status         TEXT    NOT NULL DEFAULT 'SUBMITTED'
                   CHECK (status IN ('SUBMITTED', 'EVALUATED', 'NEEDS_REVIEW')),
    submitted_at   TEXT    NOT NULL,
    updated_at     TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE (assignment_id, student_id)
);

CREATE INDEX idx_submissions_assignment ON submissions(assignment_id);
CREATE INDEX idx_submissions_student ON submissions(student_id);

CREATE TABLE evaluations (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    submission_id       INTEGER NOT NULL REFERENCES submissions(id) ON DELETE CASCADE,
    attempt             INTEGER NOT NULL DEFAULT 1,
    total_score         INTEGER NOT NULL CHECK (total_score >= 0 AND total_score <= 100),
    overall_feedback    TEXT    NOT NULL DEFAULT '',
    strengths           TEXT    NOT NULL DEFAULT '[]',
    learning_gaps       TEXT    NOT NULL DEFAULT '[]',
    suggestions         TEXT    NOT NULL DEFAULT '[]',
    confidence          TEXT    NOT NULL DEFAULT 'MEDIUM'
                        CHECK (confidence IN ('LOW', 'MEDIUM', 'HIGH')),
    method              TEXT    NOT NULL DEFAULT 'DEMO',
    approved            INTEGER NOT NULL DEFAULT 0 CHECK (approved IN (0, 1)),
    faculty_feedback    TEXT    NOT NULL DEFAULT '',
    created_at          TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE (submission_id, attempt)
);

CREATE INDEX idx_evaluations_submission ON evaluations(submission_id);

CREATE TABLE criterion_results (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    evaluation_id  INTEGER NOT NULL REFERENCES evaluations(id) ON DELETE CASCADE,
    criterion_id   INTEGER NOT NULL REFERENCES rubric_criteria(id) ON DELETE CASCADE,
    awarded_points INTEGER NOT NULL CHECK (awarded_points >= 0),
    evidence       TEXT    NOT NULL DEFAULT '',
    feedback       TEXT    NOT NULL DEFAULT '',
    UNIQUE (evaluation_id, criterion_id)
);

CREATE TABLE sessions (
    token_hash  TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  TEXT    NOT NULL,
    created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_sessions_expiry ON sessions(expires_at);

CREATE TABLE db_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
