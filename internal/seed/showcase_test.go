package seed

import (
	"path/filepath"
	"testing"

	"educore/internal/database"
)

func TestShowcaseSeedIdempotent(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "show.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close(db)
	for round := 1; round <= 3; round++ {
		if err := Run(db); err != nil {
			t.Fatalf("seed round %d: %v", round, err)
		}
	}
	var courses, assignments, students int
	if err := db.QueryRow(`SELECT COUNT(*) FROM courses`).Scan(&courses); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM assignments`).Scan(&assignments); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'STUDENT'`).Scan(&students); err != nil {
		t.Fatal(err)
	}
	if courses < 3 || assignments < 6 || students < 8 {
		t.Fatalf("showcase too small: %d courses, %d assignments, %d students", courses, assignments, students)
	}
	// stable natural keys: no duplicates after 3 rounds
	var dupes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM (SELECT course_id, title, COUNT(*) c FROM assignments GROUP BY course_id, title HAVING c > 1)`).Scan(&dupes); err != nil {
		t.Fatal(err)
	}
	if dupes != 0 {
		t.Fatalf("duplicate assignments: %d", dupes)
	}
	// approved rows must reference a valid criterion set and cover 100 max
	var bad int
	if err := db.QueryRow(`SELECT COUNT(*) FROM evaluations e
		JOIN submissions s ON s.id = e.submission_id
		WHERE e.approved = 1 AND e.total_score != (SELECT COALESCE(SUM(awarded_points),0) FROM criterion_results cr WHERE cr.evaluation_id = e.id)`).Scan(&bad); err != nil {
		t.Fatal(err)
	}
	if bad != 0 {
		t.Fatalf("%d approved evaluations deserialize inconsistently", bad)
	}
}
