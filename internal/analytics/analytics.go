// Package analytics computes every dashboard metric from faculty-approved
// evaluations only. The functions are pure: they operate on rows fetched by
// the store and are unit tested against the documented aggregation rules.
package analytics

import "sort"

// ApprovedResult is one "latest approved attempt" row for a submission.
type ApprovedResult struct {
	SubmissionID int64
	AssignmentID int64
	CourseID     int64
	StudentID    int64
	Topic        string
	Assignment   string  // title (also used when Topic is empty)
	Points       float64 // effective (approved) points
	MaxPoints    float64 // rubric max total
	ApprovedAt   string
}

// Percent normalizes points against the max; 0 when no max exists.
func (r ApprovedResult) Percent() float64 {
	if r.MaxPoints <= 0 {
		return 0
	}
	return r.Points * 100 / r.MaxPoints
}

// Rate computes submitted/expected*100 defensively and caps at 100.
func Rate(submitted, expected int) float64 {
	if expected <= 0 {
		return 0
	}
	v := float64(submitted) * 100 / float64(expected)
	if v > 100 {
		v = 100
	}
	return v
}

// studentTopics normalizes rows to per-student topic aggregation buckets.
func studentTopics(rows []ApprovedResult) map[int64]map[string][2]float64 {
	out := map[int64]map[string][2]float64{}
	for _, r := range rows {
		m := out[r.StudentID]
		if m == nil {
			m = map[string][2]float64{}
			out[r.StudentID] = m
		}
		key := r.Topic
		if key == "" {
			key = r.Assignment
		}
		v := m[key]
		v[0] += r.Points
		v[1] += r.MaxPoints
		m[key] = v
	}
	return out
}

// TopicStat aggregates one topic across students.
type TopicStat struct {
	Topic       string
	CourseID    int64
	Average     float64 // percent
	Evaluations int     // approved evaluated submissions counted
	Students    int     // distinct students contributing
	AlertLow    bool
}

// TopicMastery: for each topic, each student's percent for that topic is
// computed, then the topic average is the mean of the per-student values
// (documented rule; submission count never weighs a student twice).
func TopicMastery(rows []ApprovedResult) []TopicStat {
	totals := map[string]*[2]float64{} // topic -> [sum of student %, students with data]
	courses := map[string]int64{}
	for _, r := range rows {
		key := r.Topic
		if key == "" {
			key = r.Assignment
		}
		if courses[key] == 0 {
			courses[key] = r.CourseID
		}
	}
	topics := studentTopics(rows)
	for _, m := range topics {
		for topic, v := range m {
			if v[1] <= 0 {
				continue
			}
			tw := totals[topic]
			if tw == nil {
				tw = &[2]float64{}
				totals[topic] = tw
			}
			tw[0] += v[0] * 100 / v[1]
			tw[1]++
		}
	}
	out := make([]TopicStat, 0, len(totals))
	for topic, tw := range totals {
		st := TopicStat{Topic: topic, CourseID: courses[topic]}
		if tw[1] > 0 {
			st.Average = tw[0] / tw[1]
			st.Students = int(tw[1])
		}
		for _, r := range rows {
			k := r.Topic
			if k == "" {
				k = r.Assignment
			}
			if k == topic {
				st.Evaluations++
			}
		}
		st.AlertLow = st.Average > 0 && st.Average < 60
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Topic < out[j].Topic })
	return out
}

// CourseStat is the per-course comprehension aggregate.
type CourseStat struct {
	CourseID      int64
	Comprehension float64 // mean of per-student averages (percent)
	Students      int     // distinct students evaluated
	Evaluations   int     // approved results counted
}

// SubjectComprehension: per-student percent across the subject's evaluated
// assignments, then the mean over students (missing assignments never become
// zero marks).
func SubjectComprehension(rows []ApprovedResult, courseID int64) CourseStat {
	var courseRows []ApprovedResult
	for _, r := range rows {
		if r.CourseID == courseID {
			courseRows = append(courseRows, r)
		}
	}
	topics := studentTopics(courseRows)
	total := 0.0
	n := 0
	usedNew := map[int64]bool{}
	for student, m := range topics {
		sum := 0.0
		max := 0.0
		for _, v := range m {
			sum += v[0]
			max += v[1]
		}
		if max > 0 {
			total += sum * 100 / max
			n++
			usedNew[student] = true
		}
	}
	_ = usedNew
	return CourseStat{CourseID: courseID, Comprehension: div(total, float64(n)), Students: n, Evaluations: len(courseRows)}
}

func div(a, b float64) float64 {
	if b <= 0 {
		return 0
	}
	return a / b
}

// StudentStat is the per-student aggregate for faculty views.
type StudentStat struct {
	StudentID   int64
	AvgPercent  float64
	Evaluations int
	Strongest   string
	Weakest     string
	WeakestPct  float64
}

// StudentAverages returns per-student approved averages and topic extremes.
func StudentAverages(rows []ApprovedResult) map[int64]StudentStat {
	sums := map[int64][3]float64{} // points, max, count
	topics := studentTopics(rows)
	for _, r := range rows {
		s := sums[r.StudentID]
		s[0] += r.Points
		s[1] += r.MaxPoints
		s[2]++
		sums[r.StudentID] = s
	}
	out := map[int64]StudentStat{}
	for id, s := range sums {
		st := StudentStat{StudentID: id, Evaluations: int(s[2])}
		if s[1] > 0 {
			st.AvgPercent = s[0] * 100 / s[1]
		}
		bestPct, worstPct := -1.0, 101.0
		for t, v := range topics[id] {
			if v[1] <= 0 {
				continue
			}
			pct := v[0] * 100 / v[1]
			if pct > bestPct {
				bestPct = pct
				st.Strongest = t
			}
			if pct < worstPct {
				worstPct = pct
				st.Weakest = t
				st.WeakestPct = pct
			}
		}
		out[id] = st
	}
	return out
}
