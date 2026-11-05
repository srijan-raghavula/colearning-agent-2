package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"educore/internal/analytics"
	"educore/internal/store"
)

// StudentFacultyRow is the enriched Students page row (faculty-scoped).
type StudentFacultyRow struct {
	ID              int64
	Name            string
	Email           string
	CourseCount     int
	AvgPercent      float64
	AvgKnown        bool
	Completion      float64
	CompletionKnown bool
	Attention       string // explainable reason or ""
	WeakestTopic    string
}

// FacultyStudents lists students + creation form, with per-student academic
// aggregates computed from the faculty member's own courses only.
func (d *Deps) FacultyStudents(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	known, err := d.Store.EnrolledStudents(fid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	courses, cerr := d.Store.CoursesByFaculty(fid)
	if cerr != nil {
		http.Error(w, cerr.Error(), http.StatusInternalServerError)
		return
	}
	// Course membership (faculty scope).
	members := map[int64]map[int64]bool{}
	for _, c := range courses {
		enrolled, _ := d.Store.StudentsOfCourse(c.ID, fid)
		for _, en := range enrolled {
			m := members[en.ID]
			if m == nil {
				m = map[int64]bool{}
				members[en.ID] = m
			}
			m[c.ID] = true
		}
	}
	// Approved averages scoped to this faculty's courses.
	approved, err := d.Store.ApprovedRowsForFaculty(fid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	avgs := analytics.StudentAverages(approved)
	// Missing/overdue work per student (faculty scope).
	missing := map[int64]int{}
	overdue := map[int64]int{}
	for _, c := range courses {
		list, aerr := d.Store.AssignmentsByCourse(c.ID)
		if aerr != nil {
			continue
		}
		for _, a := range list {
			if !a.Published {
				continue
			}
			roster, err := d.Store.AssignmentRoster(a)
			if err != nil {
				continue
			}
			for _, rr := range roster {
				if rr.Status == "NOT_SUBMITTED" {
					missing[rr.StudentID]++
					if store.OverdueDue(a) {
						overdue[rr.StudentID]++
					}
				}
			}
		}
	}
	out := make([]*StudentFacultyRow, 0, len(known))
	for _, st := range known {
		ids := members[st.ID]
		published := 0
		superb := 0
		for cid := range ids {
			list, aerr := d.Store.AssignmentsByCourse(cid)
			if aerr != nil {
				continue
			}
			for _, a := range list {
				if a.Published {
					published++
					if st2, ok := statesByIDForStudent(d, st.ID, a); ok && st2 != "NOT_SUBMITTED" {
						superb++
					}
				}
			}
		}
		row := &StudentFacultyRow{
			ID:           st.ID,
			Name:         st.Name,
			Email:        st.Email,
			CourseCount:  len(ids),
			AvgPercent:   avgs[st.ID].AvgPercent,
			AvgKnown:     avgs[st.ID].Evaluations > 0,
			WeakestTopic: avgs[st.ID].Weakest,
		}
		if published > 0 {
			row.Completion = analytics.Rate(superb, published)
			row.CompletionKnown = true
		}
		if msg := attentionReason(row, missing[st.ID], overdue[st.ID]); msg != "" {
			row.Attention = msg
		}
		out = append(out, row)
	}
	p := d.pageFrom(r)
	p.Title = "Students · EDUCORE"
	p.Nav = d.facultyNav(r)
	d.applyFlash(r, &p)
	d.render(w, r, "faculty_students.html", struct {
		PageData
		Students []*StudentFacultyRow
		Courses  []*store.Course
	}{p, out, courses}, http.StatusOK)
}

// statesByIDForStudent checks the stored submission status via the roster
// loader (kept cheap; the roster was already fetched during missing counts —
// this helper is used only for per-student completion in the Students page).
func statesByIDForStudent(d *Deps, studID int64, a *store.Assignment) (string, bool) {
	sub, err := d.Store.SubmissionFor(a.ID, studID)
	if err != nil {
		return "NOT_SUBMITTED", true
	}
	switch sub.Status {
	case "EVALUATED", "NEEDS_REVIEW":
		return sub.Status, true
	}
	return "SUBMITTED", true
}

// attentionReason builds the explainable indicator strings.
func attentionReason(row *StudentFacultyRow, missingN, overdueN int) string {
	var reasons []string
	if row.AvgKnown && row.AvgPercent < 60 {
		reason := fmt.Sprintf("approved average %.0f%%", row.AvgPercent)
		if row.WeakestTopic != "" {
			reason = fmt.Sprintf("%s below 60%% — weakest topic: %s", reason, row.WeakestTopic)
		}
		reasons = append(reasons, "Repeated weak results ("+reason+")")
	}
	if overdueN > 1 {
		reasons = append(reasons, fmt.Sprintf("%d overdue missing assignments", overdueN))
	} else if overdueN == 1 {
		reasons = append(reasons, "1 overdue missing assignment")
	}
	if missingN >= 2 && overdueN < missingN {
		reasons = append(reasons, fmt.Sprintf("%d missing assignments", missingN))
	}
	if row.CompletionKnown && row.Completion < 50 && missingN > 0 {
		reasons = append(reasons, fmt.Sprintf("completion only %.0f%%", row.Completion))
	}
	if len(reasons) == 0 {
		return ""
	}
	return strings.Join(reasons, " · ")
}

var _ = fmt.Sprintf
