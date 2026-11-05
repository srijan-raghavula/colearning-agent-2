package seed

import (
	"database/sql"
	"fmt"
	"strings"
)

// seedEvidenceFromContent returns a truthful quotation drawn from the actual
// submission content, so displayed evidence genuinely exists in the text.
func seedEvidenceFromContent(content string, index, n int) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	r := []rune(content)
	span := 90
	if len(r) <= span {
		return content
	}
	// Distribute non-overlapping windows across the text.
	step := (len(r) - span) / n
	if step < 1 {
		step = 1
	}
	start := index * step
	if start+span > len(r) {
		start = len(r) - span
	}
	if start < 0 {
		start = 0
	}
	return string(r[start : start+span])
}

var _ = fmt.Sprintf

// seedCritFeedback is the criterion-level diagnostic comment shown to faculty.
func seedCritFeedback(as showAssignment, index, pts, max int) string {
	ratio := 0.0
	if max > 0 {
		ratio = float64(pts) * 100 / float64(max)
	}
	var note string
	switch {
	case ratio >= 85:
		note = "Clear, specific reasoning; keep this level of detail in future answers."
	case ratio >= 60:
		note = "Main idea present; tighten the explanation and add a concrete example."
	default:
		note = "The required reasoning is missing or too thin — review the class notes and reattempt."
	}
	return fmt.Sprintf("%s (DEMO keyword feedback; verify before publish)", note)
}

// mustE runs a seeded insert with error labeling (debug/production).
func mustE(db *sql.DB, label string, args ...any) (sql.Result, error) {
	res, err := db.Exec(label, args...)
	if err != nil {
		return res, fmt.Errorf("seed exec failed [%.60s...] %w", label, err)
	}
	return res, nil
}
