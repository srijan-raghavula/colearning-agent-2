package handlers

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"educore/internal/analytics"
	"educore/internal/charts"
	"educore/internal/store"
)

// facetCourse resolves ?course=<id> filtering.
func facetCourse(r *http.Request, courses []*store.Course) int64 {
	if v, err := idParamVar(r.URL.Query().Get("course")); err == nil && v > 0 {
		for _, c := range courses {
			if c.ID == v {
				return v
			}
		}
	}
	return 0
}

func idParamVar(s string) (int64, error) {
	var v int64
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("not a number")
		}
		v = v*10 + int64(s[i]-'0')
	}
	if v == 0 && s != "0" {
		return 0, fmt.Errorf("empty")
	}
	return v, nil
}

// dashDay returns the formatted current date for the hero.
func dashDay() string { return time.Now().Format("Monday, 02 January 2006") }

// kpi is the shape consumed by the dashboards' kpi-row.
type kpi struct {
	Icon  string
	Class string
	Label string
	Value string
	Sub   string
}

// pctClass picks a badge/value color class by threshold.
func pctClass(p float64) string {
	switch {
	case p >= 75:
		return "high"
	case p >= 60:
		return "mid"
	default:
		return "low"
	}
}

func pctLabel(p float64) string { return fmt.Sprintf("%.0f%%", p) }

// FacultyOverview is the analytics command center.
func (d *Deps) FacultyOverview(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	courses, cerr := d.Store.CoursesByFaculty(fid)
	if cerr != nil {
		http.Error(w, cerr.Error(), http.StatusInternalServerError)
		return
	}
	courseFilter := facetCourse(r, courses)
	k, err := d.Store.FacultyKPIs(fid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	approved, aerr := d.Store.ApprovedRowsForFaculty(fid)
	if aerr != nil {
		http.Error(w, aerr.Error(), http.StatusInternalServerError)
		return
	}
	if courseFilter > 0 {
		var filtered []analytics.ApprovedResult
		for _, rr := range approved {
			if rr.CourseID == courseFilter {
				filtered = append(filtered, rr)
			}
		}
		approved = filtered
	}
	topics := analytics.TopicMastery(approved)
	var bars []charts.Bar
	for _, t := range topics {
		if t.Average == 0 {
			continue // pristine topic, no data yet — do not draw a fake bar
		}
		bars = append(bars, charts.Bar{
			Label: t.Topic,
			Sub:   fmt.Sprintf("%d student(s) evaluated", t.Students),
			Pct:   t.Average,
			Extra: pctLabel(t.Average),
		})
	}
	if bars == nil {
		bars = []charts.Bar{}
	}

	completion, err := d.Store.AssignmentCompletionRows(fid, courseFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pending, _ := d.Store.PendingEvalsByAssignment(fid)
	attention, _ := d.Store.AttentionList(fid)
	deadlines, _ := d.Store.UpcomingDeadlines(fid, 14)
	activity, _ := d.Store.RecentActivity(fid, 8)

	kpiRow := []kpi{
		{Icon: "users", Class: "kpi-blue", Label: "Enrolled students", Value: nts(k.Students), Sub: "Across your active courses"},
		{Icon: "book-open", Class: "kpi-blue", Label: "Active courses", Value: nts(k.Courses), Sub: fmt.Sprintf("%d published assignment(s)", k.Assignments)},
		{Icon: "check-circle", Class: "kpi-teal", Label: "Assignment completion", Value: pctLabel(analytics.Rate(k.Submitted, k.Expected)), Sub: fmt.Sprintf("%d of %d expected submissions", k.Submitted, k.Expected)},
		{Icon: "target", Class: "kpi-teal", Label: "Avg subject comprehension", Value: pctLabel(k.Comp), Sub: "Based on approved evaluations"},
		{Icon: "hourglass", Class: "kpi-amber", Label: "Pending evaluations", Value: nts(k.PendingEval + k.PendingReview), Sub: fmt.Sprintf("%d awaiting review · %d failed", k.PendingReview, k.FailedEval)},
	}

	p := d.pageFrom(r)
	p.Title = "Overview · EDUCORE"
	p.Nav = d.facultyNav(r)
	d.applyFlash(r, &p)
	d.render(w, r, "faculty_dashboard.html", struct {
		PageData
		Day        string
		Courses    []*store.Course
		CourseID   int64
		KPIs       []kpi
		TopicBars  template.HTML
		Completion []*store.CompletionRow
		Pending    map[int64]int
		Attention  []*store.AttentionRow
		Deadlines  []*store.DeadlineRow
		Activity   []*store.RecentActivityRow
		HasTopics  bool
	}{p, dashDay(), courses, courseFilter, kpiRow,
		charts.Bars(bars), completion, pending, attention, deadlines, activity, len(bars) > 0}, http.StatusOK)
}

func nts(n int) string { return fmt.Sprintf("%d", n) }

// greetingPrefix by the wall clock.
func greetingPrefix() string {
	h := time.Now().Hour()
	switch {
	case h < 5:
		return "Good night"
	case h < 12:
		return "Good morning"
	case h < 18:
		return "Good afternoon"
	default:
		return "Good evening"
	}
}

func hasPrefixStr(s, pre string) bool { return strings.HasPrefix(s, pre) }

var _ = hasPrefixStr
