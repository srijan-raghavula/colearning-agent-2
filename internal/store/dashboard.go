package store

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"educore/internal/analytics"
)

// ApprovedRowsForFaculty loads the latest approved evaluation per submission
// across the faculty member's courses, with effective (approved) totals.
func (s *Store) ApprovedRowsForFaculty(facultyID int64) ([]analytics.ApprovedResult, error) {
	return s.approvedRows(`SELECT sub.id, a.id, a.course_id, sub.student_id, a.topic, a.title, e.approved_at,
			(SELECT COALESCE(SUM(COALESCE(ov.awarded_points, cr.awarded_points)),0)
			   FROM criterion_results cr LEFT JOIN criteria_overrides ov
			     ON ov.evaluation_id = cr.evaluation_id AND ov.criterion_id = cr.criterion_id
			  WHERE cr.evaluation_id = e.id),
			(SELECT COALESCE(SUM(c3.max_points),0) FROM criterion_results cr2 JOIN rubric_criteria c3 ON c3.id = cr2.criterion_id WHERE cr2.evaluation_id = e.id)
		FROM evaluations e
		JOIN submissions sub ON sub.id = e.submission_id
		JOIN assignments a ON a.id = sub.assignment_id
		JOIN courses c ON c.id = a.course_id
		WHERE c.faculty_id = ? AND e.approved = 1
		  AND e.attempt = (SELECT MAX(x.attempt) FROM evaluations x WHERE x.submission_id = e.submission_id AND x.approved = 1)`, facultyID)
}

// ApprovedRowsForStudent is the same view restricted to one student.
func (s *Store) ApprovedRowsForStudent(studentID int64) ([]analytics.ApprovedResult, error) {
	return s.approvedRows(`SELECT sub.id, a.id, a.course_id, sub.student_id, a.topic, a.title, e.approved_at,
			(SELECT COALESCE(SUM(COALESCE(ov.awarded_points, cr.awarded_points)),0)
			   FROM criterion_results cr LEFT JOIN criteria_overrides ov
			     ON ov.evaluation_id = cr.evaluation_id AND ov.criterion_id = cr.criterion_id
			  WHERE cr.evaluation_id = e.id),
			(SELECT COALESCE(SUM(c3.max_points),0) FROM criterion_results cr2 JOIN rubric_criteria c3 ON c3.id = cr2.criterion_id WHERE cr2.evaluation_id = e.id)
		FROM evaluations e
		JOIN submissions sub ON sub.id = e.submission_id
		JOIN assignments a ON a.id = sub.assignment_id
		WHERE sub.student_id = ? AND e.approved = 1
		  AND e.attempt = (SELECT MAX(x.attempt) FROM evaluations x WHERE x.submission_id = e.submission_id AND x.approved = 1)
		ORDER BY e.approved_at, e.id`, studentID)
}

func (s *Store) approvedRows(q string, arg any) ([]analytics.ApprovedResult, error) {
	rows, err := s.DB.Query(q, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []analytics.ApprovedResult
	for rows.Next() {
		r := analytics.ApprovedResult{}
		if err := rows.Scan(&r.SubmissionID, &r.AssignmentID, &r.CourseID, &r.StudentID, &r.Topic, &r.Assignment, &r.ApprovedAt, &r.Points, &r.MaxPoints); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FacKPIs is the faculty overview header metrics payload.
type FacKPIs struct {
	Students      int
	Courses       int
	Assignments   int // published
	Submitted     int
	Expected      int
	Comp          float64 // subject comprehension
	EvaluationsN  int
	PendingEval   int
	PendingReview int
	FailedEval    int
}

// FacultyKPIs aggregates the top-row metrics.
func (s *Store) FacultyKPIs(facultyID int64) (*FacKPIs, error) {
	k := &FacKPIs{}
	if err := s.DB.QueryRow(`SELECT COUNT(DISTINCT e.student_id), COUNT(DISTINCT c.id)
		FROM enrollments e JOIN courses c ON c.id = e.course_id WHERE c.faculty_id = ?`, facultyID).
		Scan(&k.Students, &k.Courses); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRow(`SELECT COUNT(1) FROM assignments a JOIN courses c ON c.id = a.course_id
		WHERE c.faculty_id = ? AND a.published = 1`, facultyID).Scan(&k.Assignments); err != nil {
		return nil, err
	}
	// Completion across published assignments & enrolled students.
	// Expected = Σ per-course (enrolled students × published assignments in
	// that course), which reconciles exactly with the per-assignment rows.
	if err := s.DB.QueryRow(`SELECT
			(SELECT COUNT(1) FROM submissions sub
			   JOIN assignments a2 ON a2.id = sub.assignment_id AND a2.published = 1
			   JOIN courses c2 ON c2.id = a2.course_id WHERE c2.faculty_id = ?),
			(SELECT COALESCE(SUM(x.cnt * y.pub),0) FROM
				(SELECT course_id, COUNT(*) cnt FROM enrollments x GROUP BY course_id) x
				JOIN (SELECT course_id, COUNT(*) pub FROM assignments WHERE published = 1 GROUP BY course_id) y
				  ON y.course_id = x.course_id
			 WHERE x.course_id IN (SELECT id FROM courses WHERE faculty_id = ?))
			`,
		facultyID, facultyID).
		Scan(&k.Submitted, &k.Expected); err != nil {
		return nil, err
	}
	// Subject comprehension from approved rows only.
	approved, err := s.ApprovedRowsForFaculty(facultyID)
	if err != nil {
		return nil, err
	}
	k.EvaluationsN = len(approved)
	// Evaluation workflow counters.
	if err := s.DB.QueryRow(`SELECT
			COALESCE(SUM(CASE WHEN sub.eval_status IN ('PENDING','RUNNING') THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN sub.eval_status = 'NEEDS_REVIEW' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN sub.eval_status = 'FAILED' THEN 1 ELSE 0 END),0)
		FROM submissions sub JOIN assignments a ON a.id = sub.assignment_id
		JOIN courses c ON c.id = a.course_id WHERE c.faculty_id = ?`, facultyID).
		Scan(&k.PendingEval, &k.PendingReview, &k.FailedEval); err != nil {
		return nil, err
	}
	compTotal := 0.0
	compN := 0
	courses, err := s.CoursesByFaculty(facultyID)
	if err != nil {
		return nil, err
	}
	for _, c := range courses {
		cstat := analytics.SubjectComprehension(approved, c.ID)
		if cstat.Students > 0 {
			compTotal += cstat.Comprehension
			compN++
		}
	}
	if compN > 0 {
		k.Comp = compTotal / float64(compN)
	}
	return k, nil
}

// CompletionRow is one assignment's monitoring summary for the dashboard.
type CompletionRow struct {
	AssignmentID int64
	Title        string
	CourseID     int64
	CourseCode   string
	DueDate      string
	Enrolled     int
	Submitted    int
	Missing      int
	Overdue      int
	PendingEval  int
	Percent      float64
}

// AssignmentCompletionRows summarizes every published assignment.
func (s *Store) AssignmentCompletionRows(facultyID, courseID int64) ([]*CompletionRow, error) {
	q := `SELECT a.id, a.title, a.course_id, c.code, a.due_date FROM assignments a
		JOIN courses c ON c.id = a.course_id WHERE c.faculty_id = ? AND a.published = 1`
	args := []any{facultyID}
	if courseID > 0 {
		q += ` AND a.course_id = ?`
		args = append(args, courseID)
	}
	q += ` ORDER BY a.due_date`
	var collected []struct {
		ID         int64
		Title      string
		CourseID   int64
		CourseCode string
		DueDate    string
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c struct {
			ID         int64
			Title      string
			CourseID   int64
			CourseCode string
			DueDate    string
		}
		if err := rows.Scan(&c.ID, &c.Title, &c.CourseID, &c.CourseCode, &c.DueDate); err != nil {
			rows.Close()
			return nil, err
		}
		collected = append(collected, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close() // release before the counts sub-queries

	var out []*CompletionRow
	for _, row := range collected {
		r := &CompletionRow{
			AssignmentID: row.ID, Title: row.Title, CourseID: row.CourseID,
			CourseCode: row.CourseCode, DueDate: row.DueDate,
		}
		a := &Assignment{ID: r.AssignmentID}
		counts, err := s.AssignmentCounts(a)
		if err != nil {
			continue
		}
		r.Enrolled = counts.Enrolled
		r.Submitted = counts.Submitted
		r.Missing = counts.NotSubmitted
		r.Overdue = counts.Overdue
		r.Percent = analytics.Rate(counts.Submitted, counts.Enrolled)
		out = append(out, r)
	}
	return out, nil
}

// PendingEvalsRow counts pending evaluations per assignment.
func (s *Store) PendingEvalsByAssignment(facultyID int64) (map[int64]int, error) {
	rows, err := s.DB.Query(`SELECT a.id, COUNT(1) FROM submissions sub
		JOIN assignments a ON a.id = sub.assignment_id
		JOIN courses c ON c.id = a.course_id
		WHERE c.faculty_id = ? AND sub.eval_status IN ('PENDING','RUNNING','NEEDS_REVIEW') GROUP BY a.id`, facultyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// AttentionRow explains why a student needs a look.
type AttentionRow struct {
	StudentID    int64
	StudentName  string
	StudentEmail string
	CourseName   string
	CourseCode   string
	Reason       string
	Detail       string
	Metric       string
}

// AttentionList produces explainable indicators: weakest approved topic under
// 60%, or multiple missing/overdue assignments.
func (s *Store) AttentionList(facultyID int64) ([]*AttentionRow, error) {
	approved, err := s.ApprovedRowsForFaculty(facultyID)
	if err != nil {
		return nil, err
	}
	courses, _ := s.CoursesByFaculty(facultyID)
	courseName := map[int64]*Course{}
	for _, c := range courses {
		_ = c
		courseName[c.ID] = c
	}
	// Identity is resolved for EVERY student enrolled in the faculty's courses
	// (not only those with approved evaluations) — otherwise missing-work rows
	// render with blank names/emails.
	names := map[int64][2]string{}
	{
		rows, err := s.DB.Query(`SELECT DISTINCT u.id, u.name, u.email
			FROM enrollments e JOIN users u ON u.id = e.student_id
			WHERE e.course_id IN (SELECT id FROM courses WHERE faculty_id = ?)`, facultyID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var nm, em string
			if err := rows.Scan(&id, &nm, &em); err != nil {
				rows.Close()
				return nil, err
			}
			names[id] = [2]string{nm, em}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	avgs := analytics.StudentAverages(approved)
	// derive student + course associations from rows
	studentCourse := map[int64]map[int64]bool{}
	for _, r := range approved {
		m := studentCourse[r.StudentID]
		if m == nil {
			m = map[int64]bool{}
			studentCourse[r.StudentID] = m
		}
		m[r.CourseID] = true
	}
	// Full enrollment membership (needed for missing-work rows whose students
	// have no approved evaluations yet).
	fullMembership := map[int64]map[int64]bool{}
	for _, c := range courses {
		enrolled, err := s.StudentsOfCourse(c.ID, facultyID)
		if err != nil {
			continue
		}
		for _, en := range enrolled {
			m := fullMembership[en.ID]
			if m == nil {
				m = map[int64]bool{}
				fullMembership[en.ID] = m
			}
			m[c.ID] = true
		}
	}
	var out []*AttentionRow
	for sid, st := range avgs {
		if st.Evaluations == 0 {
			continue
		}
		if st.WeakestPct < 60 && st.Weakest != "" {
			inf := names[sid]
			for cid := range studentCourse[sid] {
				c := courseName[cid]
				if c == nil {
					continue
				}
				out = append(out, &AttentionRow{
					StudentID: sid, StudentName: inf[0], StudentEmail: inf[1],
					CourseName: c.Name, CourseCode: c.Code,
					Reason: "Weak topic mastery",
					Detail: fmtWeak(st.Weakest, st.WeakestPct),
					Metric: fmtPctSql(st.WeakestPct),
				})
				break // one row per student for now
			}
		}
	}
	// Missing/overdue contributors: students with >= 2 missing across courses
	missing := map[int64]int{}
	missDue := map[int64][]string{}
	for _, c := range courses {
		published, err := s.AssignmentsByCourse(c.ID)
		if err != nil {
			continue
		}
		for _, a := range published {
			if !a.Published {
				continue
			}
			roster, err := s.AssignmentRoster(a)
			if err != nil {
				continue
			}
			for _, rrow := range roster {
				if rrow.Status != "NOT_SUBMITTED" {
					continue
				}
				missing[rrow.StudentID]++
				if OverdueDue(a) {
					missDue[rrow.StudentID] = append(missDue[rrow.StudentID], a.Title)
				}
			}
		}
	}
	for sid, n := range missing {
		if n < 2 {
			continue
		}
		if containsStudent(out, sid) {
			continue
		}
		inf := names[sid]
		var course *Course
		for cid := range fullMembership[sid] {
			if c := courseName[cid]; c != nil {
				course = c
				break
			}
		}
		if course == nil {
			course = courseByNameFallback(s, facultyID)
		}
		detail := "Multiple assignments missing"
		if len(missDue[sid]) > 0 {
			detail = "Overdue: " + strings.Join(dedupe(missDue[sid]), ", ")
		}
		if inf[0] == "" {
			continue
		}
		out = append(out, &AttentionRow{
			StudentID: sid, StudentName: inf[0], StudentEmail: inf[1],
			CourseName: course.Name, CourseCode: course.Code,
			Reason: "Missing work", Detail: detail, Metric: fmtMissing(n),
		})
	}
	if out == nil {
		out = []*AttentionRow{}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StudentName == out[j].StudentName {
			return out[i].Reason < out[j].Reason
		}
		return out[i].StudentName < out[j].StudentName
	})
	return out, nil
}

func containsStudent(rows []*AttentionRow, id int64) bool {
	for _, r := range rows {
		if r.StudentID == id {
			return true
		}
	}
	return false
}

func courseByNameFallback(s *Store, facultyID int64) *Course {
	list, _ := s.CoursesByFaculty(facultyID)
	if len(list) > 0 {
		return list[0]
	}
	return &Course{Name: "—"}
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Numeric formatting helpers (store-local to avoid template func clashes).
func fmtWeak(topic string, pct float64) string { return fmt.Sprintf("%s at %.0f%%", topic, pct) }
func fmtPctSql(p float64) string               { return fmt.Sprintf("%.0f%%", p) }
func fmtMissing(n int) string                  { return fmt.Sprintf("%d missing", n) }
func itoaI(v int) string                       { return strconv.Itoa(v) }

// DeadlineRow is an upcoming-deadline list item.
type DeadlineRow struct {
	AssignmentID int64
	Title        string
	CourseCode   string
	DueDate      string
	DueIn        string
	Outstanding  int
}

// UpcomingDeadlines lists published assignments due within the next window.
func (s *Store) UpcomingDeadlines(facultyID int64, withinDays int) ([]*DeadlineRow, error) {
	list, err := s.AssignmentsByFaculty(facultyID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var out []*DeadlineRow
	for _, a := range list {
		if !a.Published {
			continue
		}
		t, err := ParseStored(a.DueDate)
		if err != nil {
			continue
		}
		d := t.Sub(now)
		if d > time.Duration(withinDays)*24*time.Hour || d < -48*time.Hour {
			continue
		}
		counts, err := s.AssignmentCounts(a)
		if err != nil {
			continue
		}
		var code string
		_ = s.DB.QueryRow(`SELECT code FROM courses WHERE id = ?`, a.CourseID).Scan(&code)
		out = append(out, &DeadlineRow{
			AssignmentID: a.ID, Title: a.Title, CourseCode: code, DueDate: a.DueDate,
			DueIn:       humanDue(d, t),
			Outstanding: counts.NotSubmitted,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DueDate < out[j].DueDate })
	return out, nil
}

func humanDue(d time.Duration, t time.Time) string {
	if d < 0 {
		return "Overdue"
	}
	days := int(d.Hours() / 24)
	if days >= 1 {
		return "in " + itoaI(days) + " day(s)"
	}
	return "today"
}

// RecentActivityRow powers the dashboard activity table.
type RecentActivityRow struct {
	SubmissionID int64
	StudentName  string
	Assignment   string
	At           string
	EvalStatus   string
	Method       string
}

// RecentActivity lists the latest submission events for a faculty member.
func (s *Store) RecentActivity(facultyID int64, limit int) ([]*RecentActivityRow, error) {
	rows, err := s.DB.Query(`SELECT sub.id, u.name, a.title, COALESCE(sub.eval_status,'NOT_REQUESTED'),
			COALESCE(e.method,''), COALESCE(j.updated_at, sub.submitted_at)
		FROM submissions sub
		JOIN assignments a ON a.id = sub.assignment_id
		JOIN courses c ON c.id = a.course_id AND c.faculty_id = ?
		JOIN users u ON u.id = sub.student_id
		LEFT JOIN evaluation_jobs j ON j.id = (SELECT id FROM evaluation_jobs y WHERE y.submission_id = sub.id ORDER BY id DESC LIMIT 1)
		ORDER BY COALESCE(j.updated_at, sub.submitted_at) DESC LIMIT ?`, facultyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RecentActivityRow
	for rows.Next() {
		r := &RecentActivityRow{}
		if err := rows.Scan(&r.SubmissionID, &r.StudentName, &r.Assignment, &r.EvalStatus, &r.Method, &r.At); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
