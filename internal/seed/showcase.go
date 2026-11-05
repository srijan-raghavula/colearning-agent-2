package seed

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"educore/internal/auth"
)

// showcaseCourse is one seeded subject (natural key = code).
type showcaseCourse struct {
	Code, Name, Description string
	Assignments             []showAssignment
}

type showAssignment struct {
	Title, Topic, Description string
	DueOffsetDays             int // relative to now; negative = past
	Published                 bool
	Criteria                  []showCriterion
}

type showCriterion struct {
	Title, Description, Topic, Keywords string
	MaxPoints                           int
}

// Word bank reused so seeded evidence truly exists inside submissions.
var demoParagraphs = map[string]string{
	"queue":  "A breadth-first traversal processes nodes level by level using a queue and marks each vertex visited exactly once.",
	"stack":  "Depth-first search descends along one branch using a stack before unwinding the recursion step by step.",
	"pivot":  "Quicksort partitions the array around a pivot chosen from the segment, placing smaller items left and larger right in average n log n time.",
	"normal": "A relation is in second normal form when every non-key attribute depends on the whole key; third normal form additionally removes transitive dependencies such as branch -> city -> branch manager.",
	"index":  "A B-tree index narrows the search space logarithmically, so range queries scan only the matching leaves and the optimizer records the access path.",
	"txiso":  "Serializable isolation is enforced with two-phase locking: shared locks are held until commit which prevents non-repeatable reads and phantom rows.",
	"hash":   "Open addressing deals with collisions by probing the next slot with a quadratic step, keeping the load factor bounded to preserve the expected constant lookup.",
}

func showcaseCourses() []showcaseCourse {
	return []showcaseCourse{
		{
			Code: "CS201", Name: "Data Structures", Description: "Classic structures, traversal strategies and amortized analysis.",
			Assignments: []showAssignment{
				{
					Title: "Binary Tree Traversals", Topic: "Binary Trees", DueOffsetDays: 6, Published: true,
					Description: "Explain pre-order, in-order and level-order traversal of a BST and when each is preferable.",
					Criteria: []showCriterion{
						{Title: "Traversal correctness", Description: "All three orders described correctly", Topic: "Binary Trees", Keywords: "pre-order,in-order,level,queue,leaf", MaxPoints: 40},
						{Title: "Use-case reasoning", Description: "When each traversal is preferable", Topic: "Binary Trees", MaxPoints: 35},
						{Title: "Complexity clarity", Description: "Per-order complexity", Topic: "Binary Trees", MaxPoints: 25},
					},
				},
				{
					Title: "Graph Algorithms in Practice", Topic: "Graph Traversal", DueOffsetDays: -9, Published: true,
					Description: "Compare BFS and DFS and give a routing example for each.",
					Criteria: []showCriterion{
						{Title: "Algorithms correctness", Description: "BFS/DFS mechanics accurate", Topic: "Graph Traversal", Keywords: "queue,stack,level,vertex", MaxPoints: 50},
						{Title: "Practical example", Description: "Routing example grounded in reality", Topic: "Graph Traversal", MaxPoints: 30},
						{Title: "Complexity analysis", Description: "V+E analysis", Topic: "Graph Traversal", MaxPoints: 20},
					},
				},
				{
					Title: "Dynamic Programming Warm-up", Topic: "Dynamic Programming", DueOffsetDays: 12, Published: false,
					Description: "Derive the coin-change recurrence and identify overlapping subproblems.",
					Criteria: []showCriterion{
						{Title: "Recurrence", Description: "Correct DP formulation", Topic: "Dynamic Programming", Keywords: "memoization,subproblem,table", MaxPoints: 60},
						{Title: "Base cases", Description: "Boundary handling", Topic: "Dynamic Programming", MaxPoints: 40},
					},
				},
			},
		},
		{
			Code: "CS204", Name: "Databases", Description: "Relational modeling, normalization and transactional behavior.",
			Assignments: []showAssignment{
				{
					Title: "Normalization Review", Topic: "Database Normalization", DueOffsetDays: -4, Published: true,
					Description: "Given a booking schema, argue which normal forms it violates and how to decompose it.",
					Criteria: []showCriterion{
						{Title: "Normal form reasoning", Description: "2NF/3NF applied correctly", Topic: "Database Normalization", Keywords: "normal form,transitive,dependency", MaxPoints: 45},
						{Title: "Decomposition quality", Description: "Lossless decomposition proposed", Topic: "Database Normalization", MaxPoints: 30},
						{Title: "Examples", Description: "Concrete rows demonstrating the problem", Topic: "Database Normalization", MaxPoints: 25},
					},
				},
				{
					Title: "SQL Query Clinic", Topic: "SQL", DueOffsetDays: 5, Published: true,
					Description: "Rewrite slow N+1 queries using joins and an index plan.",
					Criteria: []showCriterion{
						{Title: "Index understanding", Description: "B-tree reasoning", Topic: "SQL", Keywords: "index,b-tree,leaf,query plan", MaxPoints: 40},
						{Title: "Rewrite correctness", Description: "Joins eliminate round trips", Topic: "SQL", MaxPoints: 35},
						{Title: "Trade-off discussion", Description: "Cost of denormalization", Topic: "SQL", MaxPoints: 25},
					},
				},
			},
		},
		{
			Code: "CS302", Name: "Algorithms", Description: "Sorting techniques, correctness proofs and asymptotic thinking.",
			Assignments: []showAssignment{
				{
					Title: "Sorting Showdown", Topic: "Sorting", DueOffsetDays: -3, Published: true,
					Description: "Compare quicksort with merge sort in the context of external memory.",
					Criteria: []showCriterion{
						{Title: "Partition insight", Description: "Pivot handling explained", Topic: "Sorting", Keywords: "quicksort,pivot,partition", MaxPoints: 50},
						{Title: "Complexity comparison", Description: "Worst vs average cases", Topic: "Sorting", MaxPoints: 30},
						{Title: "Memory behavior", Description: "In-place vs external memory", Topic: "Sorting", MaxPoints: 20},
					},
				},
			},
		},
	}
}

// showStudent: seeded learner accounts (Student123! unless the account exists).
type showStudent struct {
	Email, Name string
}

func showcaseStudents() []showStudent {
	return []showStudent{
		{Email: "student@educore.demo", Name: "Arjun Mehta"},
		{Email: "meera.iyer@educore.demo", Name: "Meera Iyer"},
		{Email: "rohan.verma@educore.demo", Name: "Rohan Verma"},
		{Email: "sara.khan@educore.demo", Name: "Sara Khan"},
		{Email: "dev.patel@educore.demo", Name: "Dev Patel"},
		{Email: "anaya.rao@educore.demo", Name: "Anaya Rao"},
		{Email: "kabir.nair@educore.demo", Name: "Kabir Nair"},
		{Email: "ishita.sen@educore.demo", Name: "Ishita Sen"},
	}
}

var errSeedRepeat = fmt.Errorf("idempotent skip")

// seedShowcase populates the full demo dataset idempotently.
func seedShowcase(db *sql.DB) error {
	var facID int64
	if err := db.QueryRow(`SELECT id FROM users WHERE email = ?`, FacultyEmail).Scan(&facID); err != nil {
		return fmt.Errorf("run account seeding first: %w", err)
	}
	students := map[string]int64{}
	for _, st := range showcaseStudents() {
		id, err := ensureStudent(db, st.Email, st.Name)
		if err != nil {
			return err
		}
		students[st.Email] = id
	}

	now := time.Now().UTC()
	for _, cs := range showcaseCourses() {
		var cid int64
		err := db.QueryRow(`INSERT INTO courses (faculty_id, name, code, description)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(code) DO UPDATE SET name = excluded.name WHERE false
			RETURNING id`, facID, cs.Name, cs.Code, cs.Description).Scan(&cid)
		if err != nil {
			// Existing course (no RETURNING row) — resolved by natural key.
			if lerr := db.QueryRow(`SELECT id FROM courses WHERE code = ?`, cs.Code).Scan(&cid); lerr != nil {
				return err
			}
		}
		for _, st := range showcaseStudents() {
			// enrollment set: everyone in CS201/CS204; CS302 for a subset
			if cs.Code == "CS302" && !enrolledInCS302(st.Email) {
				continue
			}
			_, err = mustE(db, `INSERT INTO enrollments (student_id, course_id) VALUES (?, ?)
				ON CONFLICT DO NOTHING`, students[st.Email], cid)
			if err != nil {
				return err
			}
		}
		for _, as := range cs.Assignments {
			aid, err := ensureAssignment(db, cid, as, now)
			if err != nil {
				return err
			}
			// submissions + evaluation per student profile
			for email, sid := range students {
				if cs.Code == "CS302" && !enrolledInCS302(email) {
					continue
				}
				plan := evalPlanFor(cs.Code, as.Title, email)
				if plan.skip {
					continue
				}
				ensureSubmission(db, aid, sid, as, plan, now)
			}
		}
		if cs.Code == "CS204" {
			if err := ensureLearningChapters(db, cid); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureLearningChapters(db *sql.DB, courseID int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRow(`SELECT COUNT(1) FROM course_chapters WHERE course_id = ?`, courseID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return tx.Commit()
	}
	chapters := []struct {
		title, description, content string
	}{
		{
			title:       "Introduction to Relational Databases",
			description: "Learn how relational databases organize information into tables and connect related records.",
			content: `## Data in related tables
A relational database stores information in tables. Each table describes one kind of thing, such as students or courses. A row is one record, and a column stores one attribute of that record.

## Connecting information
Instead of repeating course details in every student record, a database can keep students and courses in separate tables and connect them through an enrollment table. This makes relationships explicit and helps avoid duplicated data.

## Why structure matters
Clear tables and relationships make information easier to query and update. Keys identify records and provide reliable links between tables; the next chapter explores them in detail.`,
		},
		{
			title:       "Relational Model, Keys and Integrity Constraints",
			description: "Understand relations, primary and foreign keys, and the rules that keep connected data valid.",
			content: `## The relational model
A relational database organizes data into relations, commonly shown as tables. Each row represents one record and each column represents an attribute. For example, a Students table can have student_id, name, and email columns.

## Primary keys: identifying a row
A primary key is a column, or combination of columns, that uniquely identifies each row in a table. In a Students table, student_id can be the primary key. A primary key must identify one row unambiguously and cannot be NULL.

## Foreign keys: connecting tables
A foreign key is a column, or combination of columns, whose values refer to a key in another table (or to a key in the same table). For example, an Enrollments table can store student_id as a foreign key that references Students.student_id, and course_id can reference Courses.course_id.

## Integrity constraints
Integrity constraints are rules that keep data valid. NOT NULL requires a value, UNIQUE prevents duplicate values, CHECK restricts values to a condition, and a foreign-key constraint requires a referenced record to exist. Together, these rules prevent invalid or disconnected records.

## A practical example
When a student enrolls in a course, an Enrollments row records both IDs. The primary key of Enrollments identifies that enrollment; its foreign keys connect it to an existing student and course. If the referenced student or course does not exist, the foreign-key constraint rejects the invalid relationship.

## Remember
- Primary key: uniquely identifies a row in its own table.
- Foreign key: links a row to a key in a related table.
- Integrity constraints: enforce rules that keep stored data consistent.`,
		},
	}
	now := time.Now().UTC().Format(time.DateTime)
	for position, chapter := range chapters {
		if _, err := tx.Exec(`INSERT INTO course_chapters
			(course_id, title, description, content, position, published, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
			courseID, chapter.title, chapter.description, chapter.content, position+1, now, now); err != nil {
			return fmt.Errorf("seed Learning Studio chapter: %w", err)
		}
	}
	return tx.Commit()
}

// enrolledInCS302: CS302 roster stays a conversation-sized team (5 of 8).
func enrolledInCS302(email string) bool {
	switch email {
	case "student@educore.demo", "sara.khan@educore.demo", "meera.iyer@educore.demo", "anaya.rao@educore.demo", "rohan.verma@educore.demo":
		return true
	}
	return false
}

// evalPlan describes the intended workflow state per student/assignment.
type evalPlan struct {
	skip      bool
	submitted bool
	late      bool
	approved  bool
	points    int // total effective points/100
	weakTopic bool
	fail      bool
}

func evalPlanFor(code, title, email string) evalPlan {
	p := evalPlan{}
	// grass-roots rule: strong cohort + consistent dashboards
	if title == "Dynamic Programming Warm-up" {
		p.skip = true // draft, no submissions expected
		return p
	}
	switch email {
	case "ishita.sen@educore.demo":
		p.submitted = false // permanently missing (overdue demo) for past-due work
		return p
	case "sara.khan@educore.demo", "anaya.rao@educore.demo", "meera.iyer@educore.demo":
		p.submitted = true
		p.approved = true
		p.points = 88
		p.late = code == "CS204" && title == "Normalization Review"
		return p
	case "student@educore.demo": // Arjun Mehta, the demo student
		p.submitted = true
		p.points = 78
		p.weakTopic = code == "CS302" // struggles on sorting
		p.late = code == "CS201" && title == "Graph Algorithms in Practice"
		p.approved = code != "CS201" // one pending review on CS201
		return p
	case "rohan.verma@educore.demo":
		p.submitted = true
		p.points = 45
		p.weakTopic = true
		// one loud provider failure for the monitoring screen
		p.fail = code == "CS302"
		p.approved = code == "CS204"
		if code == "CS204" {
			p.points = 52
		}
		return p
	case "dev.patel@educore.demo":
		p.submitted = code != "CS201" // missing Graph Algorithms
		p.approved = code != "CS201"
		p.points = 62
		p.weakTopic = code == "CS204"
		return p
	case "kabir.nair@educore.demo":
		p.submitted = code == "CS302"
		p.approved = p.submitted
		p.points = 72
		return p
	default:
		p.skip = true
		return p
	}
}

func helperNote() string { return "" }

func ensureStudent(db *sql.DB, email, name string) (int64, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id)
	if err == nil {
		return id, nil // never touch an existing account (or its password)
	}
	hash, herr := auth.HashPassword(StudentPass)
	if herr != nil {
		return 0, herr
	}
	res, ierr := mustE(db, `INSERT INTO users (email, name, password_hash, role)
		VALUES (?, ?, ?, 'STUDENT') ON CONFLICT(email) DO NOTHING`, email, name, hash)
	if ierr != nil {
		return 0, ierr
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if lerr := db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id); lerr != nil {
			return 0, lerr
		}
		return id, nil
	}
	return res.LastInsertId()
}

func ensureAssignment(db *sql.DB, cid int64, as showAssignment, now time.Time) (int64, error) {
	var aid int64
	err := db.QueryRow(`SELECT id FROM assignments WHERE course_id = ? AND title = ?`, cid, as.Title).Scan(&aid)
	if err == nil {
		return aid, nil
	}
	due := now.Add(time.Duration(as.DueOffsetDays) * 24 * time.Hour)
	res, ierr := mustE(db, `INSERT INTO assignments (course_id, title, description, topic, due_date, published, total_marks)
		VALUES (?, ?, ?, ?, ?, ?, 100)`,
		cid, as.Title, as.Description, as.Topic, due.Format(time.DateTime), boolInt(as.Published))
	if ierr != nil {
		return 0, ierr
	}
	aid, ierr = res.LastInsertId()
	if ierr != nil {
		return 0, ierr
	}
	for i, cr := range as.Criteria {
		if _, err := mustE(db, `INSERT INTO rubric_criteria (assignment_id, position, title, description, max_points, topic, keywords)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			aid, i+1, cr.Title, cr.Description, cr.MaxPoints, cr.Topic, cr.Keywords); err != nil {
			return 0, err
		}
	}
	return aid, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ensureSubmission records the submission (natural key = (assignment, student))
// and the matching evaluation rows only when they don't exist yet.
func ensureSubmission(db *sql.DB, aid, sid int64, as showAssignment, plan evalPlan, now time.Time) {
	if !plan.submitted {
		return
	}
	var subID int64
	err := db.QueryRow(`SELECT id FROM submissions WHERE assignment_id = ? AND student_id = ?`, aid, sid).Scan(&subID)
	if err == nil {
		// Submission exists; do not overwrite a real one, but the evaluation is
		// still provisioned below only if absent.
	} else if err == sql.ErrNoRows {
		offset := -6
		if as.DueOffsetDays < 0 { // submitted a few days before the deadline
			offset = as.DueOffsetDays + 2
		} else {
			offset = 0
		}
		submittedAt := now.Add(time.Duration(offset) * 24 * time.Hour)
		if plan.late {
			submittedAt = now.Add(12 * time.Hour).Add(time.Duration(as.DueOffsetDays-2) * 24 * time.Hour)
		}
		res, ierr := mustE(db, `INSERT INTO submissions (assignment_id, student_id, content, status, submitted_at, updated_at)
			VALUES (?, ?, ?, 'SUBMITTED', ?, ?)`,
			aid, sid, demoParagraphFor(as.Topic, sid), submittedAt.Format(time.DateTime), submittedAt.Format(time.DateTime))
		if ierr != nil {
			return
		}
		subID, _ = res.LastInsertId()
	} else {
		return
	}

	// Evaluation rows.
	var countE int
	if err := db.QueryRow(`SELECT COUNT(1) FROM evaluations WHERE submission_id = ?`, subID).Scan(&countE); err == nil && countE > 0 {
		return // keep existing attempts untouched
	}

	if plan.fail {
		// Simulate an engine failure: no evaluation row, submission flagged.
		_, _ = mustE(db, `INSERT INTO evaluation_jobs (submission_id, status, error, created_at, updated_at)
			VALUES (?, 'FAILED', 'evaluation provider returned HTTP 503 after 1 retry', ?, ?)`,
			subID, now.Format(time.DateTime), now.Format(time.DateTime))
		_, _ = mustE(db, `UPDATE submissions SET eval_status = 'FAILED' WHERE id = ?`, subID)
		return
	}

	if plan.approved {
		approvedAt := now.Add(-24 * time.Hour).Format(time.DateTime)
		total := plan.points
		var facID int64
		_ = db.QueryRow(`SELECT id FROM users WHERE email = ?`, FacultyEmail).Scan(&facID)
		res, ierr := mustE(db, `INSERT INTO evaluations (submission_id, attempt, total_score, overall_feedback, strengths, learning_gaps, suggestions,
			confidence, method, approved, confidence_reason, approved_by, approved_at, created_at)
			VALUES (?, 1, ?, ?, ?, ?, ?, 'MEDIUM', 'DEMO', 1, 'Seeded showcase result; faculty approved.', ?, ?, ?)`,
			subID, total, seedOverall(as.Topic, plan.points),
			`[]`, `[\"See learning gaps\"]`, `[\"Re-attempt the weakest criterion in the next assignment\"]`,
			facID, approvedAt, approvedAt)
		if ierr != nil {
			return
		}
		evid, _ := res.LastInsertId()
		distributeCriteria(db, evid, aid, plan, as)
		_, _ = mustE(db, `UPDATE submissions SET eval_status = 'APPROVED' WHERE id = ?`, subID)
		_, _ = mustE(db, `INSERT INTO evaluation_jobs (submission_id, status, created_at, updated_at)
			VALUES (?, 'COMPLETED', ?, ?)`, subID, approvedAt, approvedAt)
		_ = log.Printf
		_ = strings.TrimSpace
		_ = err
		_ = as.Description
		_ = total
	} else {
		// provisional evaluation awaiting review (or pending engine progress)
		_, _ = mustE(db, `UPDATE submissions SET eval_status = 'NEEDS_REVIEW' WHERE id = ?`, subID)
		var facID int64
		_ = db.QueryRow(`SELECT id FROM users WHERE email = ?`, FacultyEmail).Scan(&facID)
		evidRes, ierr := mustE(db, `INSERT INTO evaluations (submission_id, attempt, total_score, overall_feedback, strengths, learning_gaps, suggestions,
			confidence, method, approved, confidence_reason, created_at)
			VALUES (?, 1, ?, ?, ?, ?, 'Shade the pending-criterion reasoning and resubmit', ?, 'DEMO', 0, 'Seeded DEMO output; keyword matcher cannot verify reasoning; review required.', ?)`,
			subID, plan.points, seedOverall(as.Topic, plan.points), `[]`, `[\"Pending faculty review\"]`,
			now.Add(-2*time.Hour).Format(time.DateTime))
		if ierr != nil {
			return
		}
		evid, _ := evidRes.LastInsertId()
		distributeCriteria(db, evid, aid, plan, as)
		_, _ = mustE(db, `INSERT INTO evaluation_jobs (submission_id, status, created_at, updated_at)
			VALUES (?, 'COMPLETED', ?, ?)`, subID, now.Add(-2*time.Hour).Format(time.DateTime), now.Add(-2*time.Hour).Format(time.DateTime))
	}
}

// demoParagraphFor builds submission content by concatenating topic words that
// will also be quoted inside criterion_results (so evidence stays truthful).
func demoParagraphFor(topic string, sid int64) string {
	var parts []string
	switch topic {
	case "Binary Trees":
		parts = []string{demoParagraphs["queue"], "Leaf ordering matters once you swap in-order for post-order walks."}
	case "Graph Traversal":
		parts = []string{demoParagraphs["queue"], demoParagraphs["stack"]}
	case "Database Normalization":
		parts = []string{demoParagraphs["normal"], "Decomposing the bookings table removes the transitive branch manager dependency."}
	case "SQL":
		parts = []string{demoParagraphs["index"], "Adding the covering index keeps the query plan on narrow leaf pages."}
	case "Sorting":
		parts = []string{demoParagraphs["pivot"]}
	default:
		parts = []string{"Generic reflective answer that still cites concrete reasoning."}
	}
	// deterministic tail variation
	if sid%2 == 0 {
		parts = append(parts, "I compared two solutions before writing this up.")
	} else {
		parts = append(parts, "I also verified both examples by hand.")
	}
	return strings.Join(parts, " ")
}

func seedOverall(topic string, points int) string {
	quality := "solid"
	if points < 60 {
		quality = "weak"
	}
	return "[" + quality + " performance] For " + topic + " the explanation covers the required dichotomy but leaves room for deeper reasoning."
}

// distributeCriteria splits effective points across criteria proportionally
// and writes criterion_results with truthful evidence quotes.
func distributeCriteria(db *sql.DB, evid, aid int64, plan evalPlan, as showAssignment) {
	var content string
	_ = db.QueryRow(`SELECT COALESCE(content,'') FROM submissions WHERE id = (SELECT submission_id FROM evaluations WHERE id = ?)`, evid).Scan(&content)
	rows, err := db.Query(`SELECT id, position, max_points FROM rubric_criteria WHERE assignment_id = ? ORDER BY position, id`, aid)
	if err != nil {
		return
	}
	type rc struct {
		id  int64
		max int
	}
	var list []rc
	for rows.Next() {
		var r rc
		if err := rows.Scan(&r.id, new(int), &r.max); err == nil {
			list = append(list, r)
		}
	}
	rows.Close()
	if len(list) == 0 {
		return
	}
	total := plan.points
	given := 0
	for i, r := range list {
		var pts int
		if i == len(list)-1 {
			pts = total - given
		} else {
			pts = (total * r.max) / 100
		}
		if pts < 0 {
			pts = 0
		}
		if pts > r.max {
			pts = r.max
		}
		given += pts
		_, _ = mustE(db, `INSERT INTO criterion_results (evaluation_id, criterion_id, awarded_points, evidence, feedback)
			VALUES (?, ?, ?, ?, ?)`,
			evid, r.id, pts, seedEvidenceFromContent(content, i, len(list)), seedCritFeedback(as, i, pts, r.max))
	}
}

var _ = time.Now
var _ = helperNote
