-- Phase 1 Learning Studio foundation: faculty-authored, course-scoped chapters.

CREATE TABLE course_chapters (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    course_id    INTEGER NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    title        TEXT    NOT NULL,
    description  TEXT    NOT NULL DEFAULT '',
    content      TEXT    NOT NULL DEFAULT '',
    position     INTEGER NOT NULL CHECK (position > 0),
    published    INTEGER NOT NULL DEFAULT 0 CHECK (published IN (0, 1)),
    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    CHECK (length(trim(title)) > 0)
);

CREATE INDEX idx_course_chapters_course_order
    ON course_chapters(course_id, position, id);
CREATE INDEX idx_course_chapters_published
    ON course_chapters(course_id, published, position, id);
