-- Phase 3 additions: submission text extraction storage + lookup indexes.

ALTER TABLE submissions ADD COLUMN extracted_text TEXT NOT NULL DEFAULT '';

-- Faculty-owned course lookups.
CREATE INDEX IF NOT EXISTS idx_courses_faculty ON courses(faculty_id);

-- Student enrollment lookups.
CREATE INDEX IF NOT EXISTS idx_enrollments_student ON enrollments(student_id);

-- Published assignments per course.
CREATE INDEX IF NOT EXISTS idx_assignments_published ON assignments(course_id, published);

-- Deadlines for overdue calculation scans.
CREATE INDEX IF NOT EXISTS idx_assignments_due ON assignments(due_date);

-- Submissions by assignment (already covered) and by student.
CREATE INDEX IF NOT EXISTS idx_submissions_student_assign ON submissions(student_id, assignment_id);
