package evaluator

import (
	"fmt"
	"strings"
)

// round2 rounds to two decimals, half-up (away from zero for negatives —
// scores are non-negative here anyway).
func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

// validateResult enforces the strict scoring contract against brandon-in.
// It mutates nothing on the input; it returns the validated result.
//
// Rules: every criterion exactly once; unknown/duplicate IDs rejected;
// missing results rejected; 0 <= points <= max; feedback required;
// total = sum of validated points; evidence verified against the submission;
// low trust flags faculty review.
func validateResult(in Input, out []CriterionResultOut, overall string, strengths, gaps, suggestions []string, confidence, confidenceReason string, method string) (*Result, error) {
	if len(in.Criteria) == 0 {
		return nil, fmt.Errorf("%w: no rubric criteria supplied", errResultInvalid)
	}
	// Build an index of the whishlist criteria.
	maxBy := map[int64]float64{}
	order := make([]int64, 0, len(in.Criteria))
	for _, c := range in.Criteria {
		if _, dup := maxBy[c.ID]; dup {
			return nil, fmt.Errorf("%w: duplicate rubric criterion %d in input", errResultInvalid, c.ID)
		}
		maxBy[c.ID] = c.MaxPoints
		order = append(order, c.ID)
	}

	seen := map[int64]bool{}
	byID := map[int64]CriterionResultOut{}
	for _, r := range out {
		if _, unknown := maxBy[r.CriterionID]; !unknown {
			return nil, fmt.Errorf("%w: unknown criterion id %d", errResultInvalid, r.CriterionID)
		}
		if seen[r.CriterionID] {
			return nil, fmt.Errorf("%w: duplicate criterion id %d in response", errResultInvalid, r.CriterionID)
		}
		seen[r.CriterionID] = true
		byID[r.CriterionID] = r
	}
	for _, id := range order {
		if !seen[id] {
			return nil, fmt.Errorf("%w: missing criterion result for id %d", errResultInvalid, id)
		}
	}

	res := &Result{Method: method, OverallFeedback: strings.TrimSpace(overall)}
	total := 0.0
	evidenceProblems := 0
	for _, id := range order {
		maxPts := maxBy[id]
		r := byID[id]
		pts := clampHalf(r.AwardedPoints, maxPts)
		total += pts
		cr := CriterionResult{CriterionID: id, AwardedPoints: pts, Feedback: strings.TrimSpace(r.Feedback)}
		// clamp warnings
		if r.AwardedPoints < 0 {
			res.Warnings = append(res.Warnings, Warning{Level: "warning", Criterion: critName(in, id), Message: fmt.Sprintf("score %.2f was below zero and was clamped to 0", r.AwardedPoints)})
		} else if r.AwardedPoints > maxPts {
			res.Warnings = append(res.Warnings, Warning{Level: "warning", Criterion: critName(in, id), Message: fmt.Sprintf("score %.2f exceeded the maximum %.0f and was clamped", r.AwardedPoints, maxPts)})
		}
		for _, ev := range r.Evidence {
			if ev.Quote == "" || ev.Explanation == "" {
				evidenceProblems++
				res.Warnings = append(res.Warnings, Warning{Level: "warning", Criterion: critName(in, id), Message: "an evidence entry was missing a quotation or explanation"})
				continue
			}
			if quoteContains(in.SubmissionText, ev.Quote) {
				cr.Evidence = append(cr.Evidence, ev)
			} else {
				evidenceProblems++
				cr.DroppedEvidence = append(cr.DroppedEvidence, ev)
			}
		}
		if len(cr.DroppedEvidence) > 0 {
			res.Warnings = append(res.Warnings, Warning{Level: "warning", Criterion: critName(in, id), Message: fmt.Sprintf("%d quoted evidence item(s) were not found in the submission and were removed", len(cr.DroppedEvidence))})
		}
		if !isMeaningfulFeedback(cr.Feedback) {
			res.Warnings = append(res.Warnings, Warning{Level: "warning", Criterion: critName(in, id), Message: "criterion feedback was missing or generic"})
			cr.Feedback = "" // no fabricated filler
		}
		res.Criteria = append(res.Criteria, cr)
	}
	res.TotalScore = round2(total)
	if res.TotalScore > 100 {
		return nil, fmt.Errorf("%w: total %.2f exceeds 100", errResultInvalid, res.TotalScore)
	}

	for _, v := range []string{res.OverallFeedback} {
		_ = v
	}
	if !isMeaningfulFeedback(res.OverallFeedback) {
		res.OverallFeedback = ""
	}
	res.Strengths = cleanList(strengths)
	res.LearningGaps = cleanList(gaps)
	res.Suggestions = cleanList(suggestions)

	switch {
	case confidence == ConfHigh || confidence == ConfMedium || confidence == ConfLow:
		res.Confidence = confidence
	case strings.EqualFold(confidence, "high"), strings.EqualFold(confidence, "medium"), strings.EqualFold(confidence, "low"):
		res.Confidence = strings.ToUpper(confidence)
	default:
		res.Warnings = append(res.Warnings, Warning{Level: "warning", Message: fmt.Sprintf("unrecognized confidence %q — treated as LOW", confidence)})
		res.Confidence = ConfLow
	}
	res.ConfidenceReason = strings.TrimSpace(confidenceReason)

	res.NeedsReview = evidenceProblems > 0 || res.Confidence == ConfLow || len(res.Warnings) > 0
	if len(byID) == 0 {
		return nil, fmt.Errorf("%w: empty evaluation", errResultInvalid)
	}
	_ = evidenceProblems
	return res, nil
}

func critName(in Input, id int64) string {
	for _, c := range in.Criteria {
		if c.ID == id {
			return c.Title
		}
	}
	return fmt.Sprintf("criterion %d", id)
}

func clampHalf(v, max float64) float64 {
	if v < 0 {
		v = 0
	}
	if v > max {
		v = max
	}
	return round2(v)
}

func isMeaningfulFeedback(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) < 8 {
		return false
	}
	generic := []string{"good work", "keep it up", "improve your understanding", "n/a", "no feedback", "none"}
	for _, g := range generic {
		if s == g {
			return false
		}
	}
	return true
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}
