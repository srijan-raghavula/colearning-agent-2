package handlers

import (
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"educore/internal/analytics"
	"educore/internal/charts"
	"educore/internal/store"
)

// StudentOverview is the personalized progress workspace.
func (d *Deps) FacultyOverviewStudent(w http.ResponseWriter, r *http.Request) { // name kept: /student route
	sid, _ := d.sp(r)
	courses, cerr := d.Store.CoursesByStudent(sid)
	if cerr != nil {
		http.Error(w, cerr.Error(), http.StatusInternalServerError)
		return
	}
	approved, aerr := d.Store.ApprovedRowsForStudent(sid)
	if aerr != nil {
		http.Error(w, aerr.Error(), http.StatusInternalServerError)
		return
	}
	publishments, perr := d.Store.PublishedAssignmentsByStudent(sid)
	if perr != nil {
		http.Error(w, perr.Error(), http.StatusInternalServerError)
		return
	}

	// KPIs (approved-only rule).
	avg := 0.0
	if len(approved) > 0 {
		sum, max := 0.0, 0.0
		for _, rr := range approved {
			sum += rr.Points
			max += rr.MaxPoints
		}
		if max > 0 {
			avg = sum * 100 / max
		}
	}
	subjectComps := 0.0
	if cn := 0; true {
		_ = cn
	}
	var compN int
	for _, c := range courses {
		cs := analytics.SubjectComprehension(approved, c.ID)
		if cs.Students > 0 {
			subjectComps += cs.Comprehension
			compN++
		}
	}
	if compN > 0 {
		subjectComps = subjectComps / float64(compN)
	}

	submittedCount := 0
	pendingSubmissions := 0
	var upcomings []*StudentUpcoming
	now := time.Now().UTC()
	states, serr := d.Store.StudentAssignmentStates(sid)
	if serr != nil {
		http.Error(w, serr.Error(), http.StatusInternalServerError)
		return
	}
	for _, st := range states {
		a := st.Assignment
		if st.Status != "NOT_SUBMITTED" {
			submittedCount++
		} else {
			pendingSubmissions++
		}
		t, terr := store.ParseStored(a.DueDate)
		if terr != nil {
			continue
		}
		if st.DuePast && st.Status != "NOT_SUBMITTED" && !st.Late && now.Sub(t) > 7*24*time.Hour {
			continue // settled history, keep off the "upcoming" list
		}
		var courseCode, courseName, faculty string
		for _, c := range courses {
			if c.ID == a.CourseID {
				courseCode, courseName, faculty = c.Code, c.Name, c.FacultyName
			}
		}
		upcomings = append(upcomings, &StudentUpcoming{
			AssignmentID: a.ID, Title: a.Title, Topic: a.Topic,
			CourseCode: courseCode, CourseName: courseName, Faculty: faculty,
			DueDate: a.DueDate, DueState: humanDueStudent(t, now, st.Status),
			Overdue:        st.Overdue,
			NeedsAttention: st.Status == "NOT_SUBMITTED", // late-but-submitted items show a Late badge instead
			Status:         st.Status,
			Late:           st.Late,
		})
	}
	// Sort: needs-attention first, then by due date.
	upcomings = trimUp(upcomings)
	sort.SliceStable(upcomings, func(i, j int) bool {
		if upcomings[i].NeedsAttention != upcomings[j].NeedsAttention {
			return upcomings[i].NeedsAttention
		}
		return upcomings[i].DueDate < upcomings[j].DueDate
	})

	// subject cards with per-course comprehension + completion
	var subjectCards []*SubjectCard
	submittedPerCourse := map[int64]int{}
	assignmentsPerCourse := map[int64]int{}
	for _, st := range states {
		assignmentsPerCourse[st.Assignment.CourseID]++
		if st.Status != "NOT_SUBMITTED" {
			submittedPerCourse[st.Assignment.CourseID]++
		}
	}
	for _, c := range courses {
		cs := analytics.SubjectComprehension(approved, c.ID)
		subjectCards = append(subjectCards, &SubjectCard{
			CourseID: c.ID, Code: c.Code, Name: c.Name, Description: c.Description,
			Comprehension: cs.Comprehension, Evaluated: cs.Evaluations,
			AssignmentsPublished: assignmentsPerCourse[c.ID],
			Completed:            submittedPerCourse[c.ID],
		})
	}

	// Recent approved evaluations (latest first).
	var recent []approvedEvalRow
	// Latest approved attempt per submission is already respected by the loader;
	// sort by approved date descending.
	for i := 0; i < len(approved); i++ {
		rr := approved[len(approved)-1-i]
		recent = append(recent, approvedEvalRow{
			Assignment: rr.Assignment, Topic: rr.Topic,
			Points: rr.Points, Max: rr.MaxPoints, Pct: rr.Percent(),
			ApprovedAt: rr.ApprovedAt,
		})
		if len(recent) >= 5 {
			break
		}
	}

	// Insights: strongest/weakest topics across approved results.
	topics := analytics.TopicMastery(approved)
	var strongest, weakest *analytics.TopicStat
	for i := range topics {
		if topics[i].Average == 0 {
			continue
		}
		if strongest == nil || topics[i].Average > strongest.Average {
			strongest = &topics[i]
		}
		if weakest == nil || topics[i].Average < weakest.Average {
			weakest = &topics[i]
		}
	}
	var subjectBars []charts.Bar
	for _, c := range subjectCards {
		if c.Comprehension == 0 {
			continue
		}
		subjectBars = append(subjectBars, charts.Bar{Label: c.Name, Sub: c.Code, Pct: c.Comprehension, Extra: pctLabel(c.Comprehension)})
	}

	kpiRow := []kpi{
		{Icon: "award", Class: "kpi-teal", Label: "Average approved marks", Value: avgLabel(avg, len(approved)), Sub: "Only faculty-approved results"},
		{Icon: "target", Class: "kpi-blue", Label: "Subject comprehension", Value: avgLabel(subjectComps, len(approved)), Sub: "Across enrolled subjects"},
		{Icon: "check-circle", Class: "kpi-blue", Label: "Assignment completion", Value: pctLabel(analytics.Rate(submittedCount, len(publishments))), Sub: fmt.Sprintf("%d of %d published", submittedCount, len(publishments))},
		{Icon: "clock", Class: "kpi-amber", Label: "Pending submissions", Value: nts(pendingSubmissions), Sub: "Awaiting your answer"},
	}

	p := d.pageFrom(r)
	p.Title = "Overview · EDUCORE"
	p.Nav = d.studentNav(r)
	d.applyFlash(r, &p)
	p.Values = map[string]string{}
	d.render(w, r, "student_dashboard.html", struct {
		PageData
		Message     string
		KPIs        []kpi
		Subjects    []*SubjectCard
		Upcoming    []*StudentUpcoming
		Recent      []approvedEvalRow
		Strongest   *analytics.TopicStat
		Weakest     *analytics.TopicStat
		SubjectBars template.HTML
		HasApproved bool
	}{p, greetingPrefix(), kpiRow, subjectCards, upcomings, recent, strongest, weakest, charts.Bars(subjectBars), len(approved) > 0}, http.StatusOK)
}

func avgLabel(p float64, n int) string {
	if n == 0 {
		return "—"
	}
	return pctLabel(p)
}

// approvedEvalRow is the dashboard "recent evaluations" entry.
type approvedEvalRow struct {
	Assignment string
	Topic      string
	Points     float64
	Max        float64
	Pct        float64
	ApprovedAt string
}

// SubjectCard is a dashboard subject overview card.
type SubjectCard struct {
	CourseID             int64
	Code                 string
	Name                 string
	Description          string
	Comprehension        float64
	Evaluated            int
	AssignmentsPublished int
	Completed            int
}

// StudentUpcoming is an assignment surfaced by the upcoming list.
type StudentUpcoming struct {
	AssignmentID   int64
	Title          string
	Topic          string
	CourseCode     string
	CourseName     string
	Faculty        string
	DueDate        string
	DueState       string
	Status         string
	Overdue        bool
	NeedsAttention bool
	Late           bool
}

// humanDueStudent label keeps overdue clearly tied to missing work; late-but-
// submitted arrivals are handled with a separate Late badge.
func humanDueStudent(t, now time.Time, status string) string {
	d := t.Sub(now)
	if d < 0 {
		return "deadline passed"
	}
	if d < 24*time.Hour {
		return "due within 24h"
	}
	return fmt.Sprintf("due in %d day(s)", int(d.Hours()/24))
}

// trimUp aliases unused local to keep the compiler honest after edits.
func trimUp(in []*StudentUpcoming) []*StudentUpcoming { return in }

// StudentPerformance is the full analytics page for a student.
func (d *Deps) StudentPerformance(w http.ResponseWriter, r *http.Request) {
	sid, _ := d.sp(r)
	courses, _ := d.Store.CoursesByStudent(sid)
	approved, err := d.Store.ApprovedRowsForStudent(sid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// A: subject comparison.
	var subjectBars []charts.Bar
	for _, c := range courses {
		cs := analytics.SubjectComprehension(approved, c.ID)
		if cs.Students == 0 {
			continue
		}
		subjectBars = append(subjectBars, charts.Bar{Label: c.Name, Sub: fmt.Sprintf("%s · %d evaluated", c.Code, cs.Evaluations), Pct: cs.Comprehension, Extra: pctLabel(cs.Comprehension)})
	}

	// B: topic mastery grouped by course.
	topics := analytics.TopicMastery(approved)
	var topicBars []charts.Bar
	var weakTopics []*analytics.TopicStat
	for _, t := range topics {
		if t.Average == 0 {
			continue
		}
		var cname string
		for _, c := range courses {
			if c.ID == t.CourseID {
				cname = c.Name
			}
		}
		topicBars = append(topicBars, charts.Bar{Label: t.Topic, Sub: fmt.Sprintf("%s · %d evaluated", cname, t.Students), Pct: t.Average, Extra: pctLabel(t.Average)})
		if t.Average < 60 {
			weakTopics = append(weakTopics, &t)
		}
	}

	// C: trend from approved history (sorted by approval time).
	var pts []charts.LinePt
	var evals []approvedEvalRow
	for _, rr := range approved {
		t, terr := time.Parse(time.DateTime, rr.ApprovedAt)
		if terr != nil {
			continue
		}
		pts = append(pts, charts.LinePt{X: t.Unix(), Y: rr.Percent()})
		evals = append(evals, approvedEvalRow{Assignment: rr.Assignment, Topic: rr.Topic, Points: rr.Points, Max: rr.MaxPoints, Pct: rr.Percent(), ApprovedAt: rr.ApprovedAt})
	}
	trend := charts.Line(pts)
	hasTrend := len(pts) >= 2

	// D/E: gaps and strengths from the topic statistics.
	var gaps, strengths []string
	for _, t := range topics {
		if t.Average == 0 {
			continue
		}
		if t.Average < 60 {
			gaps = append(gaps, fmt.Sprintf("%s — %.0f%% across %d evaluated submission(s); revise the fundamentals and re-test with the next assignment", t.Topic, t.Average, t.Evaluations))
		} else if t.Average >= 75 {
			strengths = append(strengths, fmt.Sprintf("%s — %.0f%% maintained over %d evaluated submission(s)", t.Topic, t.Average, t.Evaluations))
		}
	}
	if len(gaps) == 0 {
		gaps = []string{"No approved topic currently falls below the 60% threshold."}
	}
	if len(strengths) == 0 {
		strengths = []string{"Approved results do not yet show a consistent strength area."}
	}

	p := d.pageFrom(r)
	p.Title = "Performance · EDUCORE"
	p.Nav = d.studentNav(r)
	p.Values = map[string]string{}
	d.render(w, r, "student_performance.html", struct {
		PageData
		SubjectBars template.HTML
		TopicBars   template.HTML
		Trend       template.HTML
		HasTrend    bool
		Approved    []approvedEvalRow
		Gaps        []string
		Strengths   []string
	}{p, charts.Bars(subjectBars), charts.Bars(topicBars), trend, hasTrend, evals, gaps, strengths}, http.StatusOK)
}

// hasPrefixStr aliases for template use; kept local in dashboard.go.
var _ = strings.HasPrefix
var _ = trimUp
