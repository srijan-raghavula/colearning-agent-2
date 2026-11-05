package evaluator

import (
	"sort"
	"strings"
	"unicode"
)

// DemoEvaluate is a deterministic, keyword-based scorer. It is NOT a language
// model: it merely counts rubric keywords present in the submission and
// derives a proportional score. It cannot represent conceptual understanding
// and always requires faculty review.
func DemoEvaluate(in Input) (*Result, error) {
	norm := normalizeForQuote(in.SubmissionText)
	words := wordSet(norm)

	outs := make([]CriterionResultOut, 0, len(in.Criteria))
	totalKeywords := 0
	matchedKeywords := 0
	var matchedAll, missedAll []string

	for _, c := range in.Criteria {
		keywords := parseKeywords(c.Keywords)
		totalKeywords += len(keywords)
		matched, missed := filterMatched(words, keywords, norm)
		matchedKeywords += len(matched)
		matchedAll = append(matchedAll, matched...)
		missedAll = append(missedAll, missed...)

		var pts float64
		var fb strings.Builder
		if len(keywords) > 0 {
			ratio := float64(len(matched)) / float64(len(keywords))
			pts = roundHalf(c.MaxPoints * ratio)
			if len(matched) > 0 {
				fb.WriteString("Keyword coverage: " + strings.Join(matched, ", "))
				fb.WriteString(" (found in the submission). ")
			}
			if len(missed) > 0 {
				fb.WriteString("Not found: " + strings.Join(missed, ", ") + ". ")
			}
			if ratio >= 0.75 {
				fb.WriteString("Most required terms appear in the answer.")
			} else if ratio >= 0.35 {
				fb.WriteString("Partial term coverage suggests the topic is partly addressed.")
			} else {
				fb.WriteString("Little or none of the expected vocabulary appears, so understanding could not be demonstrated.")
			}
		} else {
			// Documented conservative heuristic without keywords: credit for
			// a substantive, non-empty answer tied to the criterion's topic
			// words only; without keywords the engine cannot verify
			// understanding, so no score is invented beyond a floor.
			if substantive(norm) && topicWordsPresent(c, norm) {
				pts = roundHalf(c.MaxPoints * 0.4)
				fb.WriteString("The answer is substantive and mentions the criterion topic, but this criterion has no evaluation keywords, so a conservative floor (40%) was applied. Not evidence of understanding.")
			} else if substantive(norm) {
				pts = roundHalf(c.MaxPoints * 0.2)
				fb.WriteString("The answer is substantive but does not evidently address this criterion topic. Conservative floor applied; faculty should confirm.")
			} else {
				pts = 0
				fb.WriteString("The submission lacks enough content for this criterion to be scored.")
			}
		}

		// Deterministic evidence: quote actual matched phrases cut from the
		// submission; only quoted substrings that truly occur are kept.
		var ev []Evidence
		if q := firstQuote(norm, matched, in.SubmissionText); q != "" {
			ev = append(ev, Evidence{
				Quote:       q,
				Explanation: "Exact normalized substring from the student submission containing the matched keyword; proximity alone does not prove understanding.",
			})
		}
		if len(matched) == 0 {
			fb.WriteString(" DEMO mode: no strong basis; faculty verification is required.")
		}
		outs = append(outs, CriterionResultOut{
			CriterionID:   c.ID,
			AwardedPoints: pts,
			Evidence:      ev,
			Feedback:      fb.String(),
		})
	}

	overall := buildOverall(totalKeywords, matchedKeywords, matchedAll, missedAll, in)
	strengths, gaps, suggestions := demoLists(matchedAll, missedAll)

	return validateResult(in, outs, overall, strengths, gaps, suggestions, ConfLow, "DEMO keyword matching cannot assess reasoning depth; faculty review required.", MethodDemo)
}

// roundHalf rounds to nearest integer-ish 2dp.
func roundHalf(v float64) float64 { return round2(v) }

func parseKeywords(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	var out []string
	for _, p := range parts {
		if t := strings.TrimSpace(strings.ToLower(p)); t != "" {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

func wordSet(norm string) map[string]bool {
	set := map[string]bool{}
	start := -1
	for i := 0; i <= len(norm); i++ {
		if i == len(norm) || norm[i] == ' ' {
			if start >= 0 {
				set[norm[start:i]] = true
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	return set
}

// filterMatched: keyword counts as matched when the normalized phrase appears
// anywhere (covers multi-word keywords like "depth first search").
func filterMatched(words map[string]bool, keywords []string, norm string) (matched, missed []string) {
	for _, k := range keywords {
		if strings.Contains(norm, k) || words[strings.Join(strings.Fields(k), "")] {
			matched = append(matched, k)
		} else {
			missed = append(missed, k)
		}
	}
	return
}

func substantive(norm string) bool {
	return len([]rune(norm)) >= 120 && len(strings.Fields(norm)) >= 25
}

func topicWordsPresent(c CriterionInput, norm string) bool {
	for _, w := range strings.Fields(normalizeForQuote(c.Topic)) {
		if len(w) > 3 && strings.Contains(norm, w) {
			return true
		}
	}
	for _, w := range strings.Fields(normalizeForQuote(c.Title)) {
		if len(w) > 3 && strings.Contains(norm, w) {
			return true
		}
	}
	return false
}

func firstQuote(norm string, matched []string, original string) string {
	for _, k := range matched {
		if idx := strings.Index(norm, k); idx >= 0 {
			// expand ±30 chars for context window
			start := idx - 40
			if start < 0 {
				start = 0
			}
			end := idx + len(k) + 40
			if end > len(norm) {
				end = len(norm)
			}
			q := norm[start:end]
			if quoteContains(original, q) {
				return q
			}
			if quoteContains(original, k) {
				return k
			}
		}
	}
	return ""
}

func buildOverall(totalKeywords, matchedKeywords int, matched, missed []string, in Input) string {
	var b strings.Builder
	if in.ExtractionOK {
		b.WriteString("[DEMO evaluation — deterministic keyword matching, not AI] ")
	} else {
		b.WriteString("[DEMO evaluation — the attached document could not be read, so only the text below (if any) was analyzed] ")
	}
	if totalKeywords == 0 {
		b.WriteString("No evaluation keywords were defined in the rubric, so only conservative floors were applied. Faculty must grade this submission directly.")
		return b.String()
	}
	pct := 0
	if totalKeywords > 0 {
		pct = matchedKeywords * 100 / totalKeywords
	}
	b.WriteString("The submission matched " + itoa(matchedKeywords) + " of " + itoa(totalKeywords) + " rubric keywords (" + itoa(pct) + "%). ")
	if len(missed) > 0 {
		b.WriteString("Missing terms: " + strings.Join(uniqueStrings(missed), ", ") + ". ")
	}
	b.WriteString("Keyword matching does not establish conceptual understanding — treat these scores as provisional until faculty review.")
	return b.String()
}

func demoLists(matched, missed []string) (strengths, gaps, suggestions []string) {
	um := uniqueStrings(matched)
	if len(um) > 0 {
		strengths = append(strengths, "Used vocabulary aligned with "+strings.Join(um[:intMin(3, len(um))], ", "))
	}
	if m := uniqueStrings(missed); len(m) > 0 {
		gaps = append(gaps, "Expected terms did not appear in the answer: "+strings.Join(m[:intMin(5, len(m))], ", "))
		suggestions = append(suggestions, "Rewrite the explanation so the key concepts ("+strings.Join(m[:intMin(5, len(m))], ", ")+") are explicitly addressed")
	}
	suggestions = append(suggestions, "Faculty review is required: keyword matching cannot judge reasoning")
	return
}

func uniqueStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	sort.Strings(in)
	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}

func intMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = unicode.IsLetter

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
