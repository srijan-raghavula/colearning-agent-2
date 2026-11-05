package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"educore/internal/evaluator"
	"educore/internal/store"
)

// Tools for the EVALUATE / RECONSIDER runs.
const (
	ToolGetContext   = "get_assignment_context"
	ToolInspect      = "inspect_submission"
	ToolSearch       = "search_submission"
	ToolEvaluateCrit = "evaluate_criterion"
	ToolVerify       = "verify_evaluation"
	ToolSave         = "save_pending_evaluation"
)

// Tool is one registered capability.
type Tool struct {
	Name        string
	Description string
	Schema      string // JSON schema fragment for parameters
	Args        func() any
	Fn          func(ac *AgentContext, raw json.RawMessage) (any, error)
}

// AgentContext carries the authorization scope and mutable run state for the
// tool layer. Tools only ever see what the owning faculty member may see.
type AgentContext struct {
	Store       *store.Store
	FacultyID   int64
	Sub         *store.Submission
	Assignment  *store.Assignment
	RunKind     string
	Readonly    bool
	OutputChars int // budget consumed so far

	Draft DraftEval // accumulated per-criterion results (persisted between turns)
}

// DraftEval is the persisted partial evaluation.
type DraftEval struct {
	Overall     string                   `json:"overall_feedback"`
	Strengths   []string                 `json:"strengths"`
	Gaps        []string                 `json:"learning_gaps"`
	Suggestions []string                 `json:"suggested_improvements"`
	Confidence  string                   `json:"confidence"`
	Reason      string                   `json:"confidence_reason"`
	ByCriterion map[int64]DraftCriterion `json:"criteria"`
}

// DraftCriterion is one scored criterion in the draft.
type DraftCriterion struct {
	Points   float64              `json:"awarded_points"`
	Evidence []evaluator.Evidence `json:"evidence"`
	Feedback string               `json:"feedback"`
	Verified bool                 `json:"verified"`
}

// registered list (order matters for the system prompt).
func baseTools() []Tool {
	return []Tool{
		{
			Name:        ToolGetContext,
			Description: "Retrieve the assignment instructions, rubric criteria with maximum points and keywords, course name and topic.",
			Schema:      `{"type":"object","properties":{},"additionalProperties":false}`,
			Fn:          toolGetContext,
		},
		{
			Name:        ToolInspect,
			Description: "Retrieve the full submission metadata plus the extracted text (and any extraction warnings). Use this before scoring.",
			Schema:      `{"type":"object","properties":{},"additionalProperties":false}`,
			Fn:          toolInspect,
		},
		{
			Name:        ToolSearch,
			Description: "Search the submission for passages relevant to a rubric criterion. Returns verbatim snippets with approximate location offsets. Use for every criterion before scoring it.",
			Schema:      `{"type":"object","properties":{"query":{"type":"string","minLength":2,"maxLength":120}},"required":["query"],"additionalProperties":false}`,
			Fn:          toolSearch,
		},
		{
			Name:        ToolEvaluateCrit,
			Description: "Record your score for ONE rubric criterion with verbatim evidence quotes from the submission and diagnostic feedback. Quotes must exist in the submission text.",
			Schema:      `{"type":"object","properties":{"criterion_id":{"type":"integer"},"awarded_points":{"type":"number"},"evidence":{"type":"array","items":{"type":"object","properties":{"quote":{"type":"string"},"explanation":{"type":"string"}},"required":["quote","explanation"]}},"feedback":{"type":"string","maxLength":800}},"required":["criterion_id","awarded_points","evidence","feedback"],"additionalProperties":false}`,
			Fn:          toolEvaluateCriterion,
		},
		{
			Name:        ToolVerify,
			Description: "Validate the draft: criterion coverage, score bounds, total calculation and that all evidence quotes exist in the submission. Fix reported issues before saving.",
			Schema:      `{"type":"object","properties":{},"additionalProperties":false}`,
			Fn:          toolVerify,
		},
		{
			Name:        ToolSave,
			Description: "Persist the completed evaluation as a PENDING review for faculty. Requires overall_feedback, strengths, learning_gaps, suggestions, confidence and reason.",
			Schema:      `{"type":"object","properties":{"overall_feedback":{"type":"string"},"strengths":{"type":"array","items":{"type":"string"}},"learning_gaps":{"type":"array","items":{"type":"string"}},"suggested_improvements":{"type":"array","items":{"type":"string"}},"confidence":{"type":"string","enum":["HIGH","MEDIUM","LOW"]},"confidence_reason":{"type":"string"}},"required":["overall_feedback","strengths","learning_gaps","suggested_improvements","confidence","confidence_reason"],"additionalProperties":false}`,
			Fn:          toolSavePending,
		},
	}
}

// followUpTools is the read-only subset allowed in follow-up conversations.
func followUpTools() []Tool {
	var out []Tool
	for _, t := range baseTools() {
		if t.Name == ToolEvaluateCrit || t.Name == ToolVerify || t.Name == ToolSave {
			continue
		}
		out = append(out, t)
	}
	return out
}

// SpecCandidates returns the OpenAI tool declarations.
func specCandidates(all []Tool) []evaluator.ToolSpec {
	var out []evaluator.ToolSpec
	for _, t := range all {
		var s evaluator.ToolSpec
		s.Type = "function"
		s.Function.Name = t.Name
		s.Function.Description = t.Description
		s.Function.Parameters = json.RawMessage(t.Schema)
		out = append(out, s)
	}
	return out
}

// ---------- tool implementations (store-backed, authorization-scoped) ----

type emptyArgs struct{}

type searchArgs struct {
	Query string `json:"query"`
}

type evalCritArgs struct {
	CriterionID   int64                `json:"criterion_id"`
	AwardedPoints float64              `json:"awarded_points"`
	Evidence      []evaluator.Evidence `json:"evidence"`
	Feedback      string               `json:"feedback"`
}

type saveArgs struct {
	Overall     string   `json:"overall_feedback"`
	Strengths   []string `json:"strengths"`
	Gaps        []string `json:"learning_gaps"`
	Suggestions []string `json:"suggested_improvements"`
	Confidence  string   `json:"confidence"`
	Reason      string   `json:"confidence_reason"`
}

// submissionText picks the extracted text with a fallback to the plain content.
func submissionText(sub *store.Submission) (string, bool) {
	extraOK := sub.ExtractedText != "" && !strings.HasPrefix(sub.ExtractedText, "EXTRACTION_FAILED")
	if extraOK {
		return sub.ExtractedText, true
	}
	return sub.Content, false
}

func toolGetContext(ac *AgentContext, _ json.RawMessage) (any, error) {
	type crit struct {
		ID        int64   `json:"criterion_id"`
		Title     string  `json:"title"`
		Desc      string  `json:"description"`
		MaxPoints float64 `json:"max_points"`
		Topic     string  `json:"topic"`
		Keywords  string  `json:"keywords"`
	}
	type out struct {
		AssignmentID   int64  `json:"assignment_id"`
		Title          string `json:"title"`
		Instructions   string `json:"instructions"`
		Topic          string `json:"topic"`
		Course         string `json:"course"`
		CourseCode     string `json:"course_code"`
		TotalMarks     int    `json:"total_marks"`
		DueDate        string `json:"due_date"`
		Published      bool   `json:"published"`
		RubricCriteria []crit `json:"rubric_criteria"`
	}
	if ac.Assignment == nil {
		return nil, fmt.Errorf("assignment context unavailable")
	}
	var courseName, code, _, _, _ = "", "", "", "", ""
	_ = code
	if qerr := ac.Store.DB.QueryRow(`SELECT code, name FROM courses WHERE id = ?`, ac.Assignment.CourseID).Scan(&code, &courseName); qerr != nil {
		return nil, fmt.Errorf("course lookup: %w", qerr)
	}
	o := out{
		AssignmentID: ac.Assignment.ID, Title: ac.Assignment.Title,
		Instructions: ac.Assignment.Description, Topic: ac.Assignment.Topic,
		Course: courseName, CourseCode: code, TotalMarks: ac.Assignment.TotalMarks,
		DueDate: ac.Assignment.DueDate, Published: ac.Assignment.Published,
	}
	for _, c := range ac.Assignment.Criteria {
		o.RubricCriteria = append(o.RubricCriteria, crit{
			ID: c.ID, Title: c.Title, Desc: c.Description, MaxPoints: float64(c.MaxPoints),
			Topic: c.Topic, Keywords: c.Keywords,
		})
	}
	return o, nil
}

func toolInspect(ac *AgentContext, _ json.RawMessage) (any, error) {
	text, extractionOK := submissionText(ac.Sub)
	type out struct {
		SubmissionID   int64  `json:"submission_id"`
		StudentID      int64  `json:"student_id"`
		SubmittedAt    string `json:"submitted_at"`
		HasFile        bool   `json:"has_file"`
		Filename       string `json:"filename"`
		TextLength     int    `json:"text_length_chars"`
		ExtractionOK   bool   `json:"extraction_ok"`
		ExtractionNote string `json:"extraction_note,omitempty"`
		Text           string `json:"text"`
	}
	o := out{
		SubmissionID: ac.Sub.ID, StudentID: ac.Sub.StudentID,
		SubmittedAt:  ac.Sub.SubmittedAt,
		HasFile:      ac.Sub.FilePath != "",
		Filename:     ac.Sub.Filename,
		TextLength:   len([]rune(text)),
		ExtractionOK: extractionOK || strings.TrimSpace(text) != "",
		Text:         clipForModel(text, 24000),
	}
	if !extractionOK && strings.HasPrefix(ac.Sub.ExtractedText, "EXTRACTION_FAILED") {
		o.ExtractionNote = ac.Sub.ExtractedText
	}
	return o, nil
}

func clipForModel(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…[clipped]"
}

// toolSearch: substring retrieval over the actual submission text with
// offsets, so the agent never has to guess the location of its evidence.
func toolSearch(ac *AgentContext, raw json.RawMessage) (any, error) {
	var args searchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	q := strings.ToLower(strings.TrimSpace(args.Query))
	if q == "" || len(q) < 2 {
		return nil, fmt.Errorf("query must be at least 2 characters")
	}
	text, _ := submissionText(ac.Sub)
	lower := strings.ToLower(text)
	var hits []map[string]any
	for idx := 0; idx < len(lower); {
		found := strings.Index(lower[idx:], q)
		if found < 0 {
			break
		}
		start := idx + found
		window := start - 70
		if window < 0 {
			window = 0
		}
		end := start + len(q) + 70
		if end > len(text) {
			end = len(text)
		}
		hits = append(hits, map[string]any{
			"offset":   start,
			"location": fmt.Sprintf("submission text, character offset %d", start),
			"snippet":  text[window:end],
		})
		idx = start + len(q)
		if len(hits) >= 8 {
			break
		}
	}
	if hits == nil {
		hits = []map[string]any{}
	}
	return map[string]any{"query": args.Query, "matches": hits, "total_matches": len(hits)}, nil
}

// toolEvaluateCriterion validates and stores one draft criterion result.
func toolEvaluateCriterion(ac *AgentContext, raw json.RawMessage) (any, error) {
	var args evalCritArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	text := ""
	if t, _ := submissionText(ac.Sub); strings.TrimSpace(t) != "" {
		text = t
	}
	var crit *store.RubricCriterion
	for _, c := range ac.Assignment.Criteria {
		if c.ID == args.CriterionID {
			crit = c
			break
		}
	}
	if crit == nil {
		return nil, fmt.Errorf("unknown criterion_id %d", args.CriterionID)
	}
	if args.AwardedPoints < 0 {
		return nil, fmt.Errorf("awarded_points cannot be negative")
	}
	if pts := float64(crit.MaxPoints); args.AwardedPoints > pts {
		return nil, fmt.Errorf("awarded_points %.2f exceeds max_points %.0f", args.AwardedPoints, pts)
	}
	if strings.TrimSpace(args.Feedback) == "" {
		return nil, fmt.Errorf("feedback is required")
	}
	verified, dropped := 0, 0
	var kept []evaluator.Evidence
	for _, ev := range args.Evidence {
		if ev.Quote == "" || ev.Explanation == "" {
			return nil, fmt.Errorf("evidence entries require quote and explanation")
		}
		if evaluator.QuoteContains(text, ev.Quote) {
			verified++
			kept = append(kept, ev)
		} else {
			dropped++
		}
	}
	if verified == 0 {
		return nil, fmt.Errorf("no evidence quote found in the submission — quotes must be copied verbatim; please re-check with search_submission")
	}
	prev := ac.Draft.ByCriterion[args.CriterionID]
	ac.Draft.ByCriterion[args.CriterionID] = DraftCriterion{
		Points: args.AwardedPoints, Evidence: kept, Feedback: args.Feedback, Verified: true,
	}
	res := map[string]any{
		"criterion_id":      args.CriterionID,
		"awarded_points":    args.AwardedPoints,
		"verified_evidence": verified,
	}
	if dropped > 0 {
		res["dropped_fabricated_quotes"] = dropped
		res["warning"] = "Some quotes did not exist in the submission and were rejected — outcome note: reuse the surviving quotes only."
	}
	if prev.Verified {
		res["note"] = "a previous draft score for this criterion was replaced"
	}
	return res, nil
}

// toolVerify: coverage, bounds, total, evidence existence in one report.
func toolVerify(ac *AgentContext, _ json.RawMessage) (any, error) {
	text, _ := submissionText(ac.Sub)
	covered := map[int64]bool{}
	total := 0.0
	var issues []string
	for _, c := range ac.Assignment.Criteria {
		dc, ok := ac.Draft.ByCriterion[c.ID]
		if !ok {
			issues = append(issues, fmt.Sprintf("criterion %q (%d) has no score yet", c.Title, c.ID))
			continue
		}
		covered[c.ID] = true
		if dc.Points < 0 || dc.Points > float64(c.MaxPoints) {
			issues = append(issues, fmt.Sprintf("criterion %q points %.2f out of range [0,%.0f]", c.Title, dc.Points, float64(c.MaxPoints)))
		}
		total += dc.Points
		for _, ev := range dc.Evidence {
			if !evaluator.QuoteContains(text, ev.Quote) {
				issues = append(issues, fmt.Sprintf("criterion %q evidence quote not found verbatim: %.40s", c.Title, ev.Quote))
			}
		}
	}
	if total > 100.0001 {
		issues = append(issues, fmt.Sprintf("total %.2f exceeds 100", total))
	}
	for id := range ac.Draft.ByCriterion {
		if !covered[id] {
			issues = append(issues, fmt.Sprintf("draft references unknown criterion %d", id))
		}
	}
	if len(issues) > 0 {
		return map[string]any{"valid": false, "issues": issues, "draft_total": totalRound(total)}, nil
	}
	return map[string]any{
		"valid": true, "criterion_covered": len(ac.Assignment.Criteria), "total": totalRound(total),
		"note": "ready for save_pending_evaluation",
	}, nil
}

func totalRound(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }

// toolSavePending composes the final evaluation via the production
// validation pipeline and persists it WITHOUT approval.
func toolSavePending(ac *AgentContext, raw json.RawMessage) (any, error) {
	if ac.Readonly {
		return nil, fmt.Errorf("save_pending_evaluation is not available in follow-up conversations; scores can only change through a reconsideration run or the faculty grading workspace")
	}
	if len(ac.Draft.ByCriterion) == 0 {
		return nil, fmt.Errorf("no criterion scores recorded yet — call evaluate_criterion first")
	}
	var args saveArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	switch strings.ToUpper(args.Confidence) {
	case "HIGH", "MEDIUM", "LOW":
	default:
		return nil, fmt.Errorf("confidence must be HIGH, MEDIUM or LOW")
	}
	// Build engine input for validation rules.
	in := evaluator.Input{
		AssignmentID: ac.Assignment.ID,
		SubmissionID: ac.Sub.ID,
		Topic:        ac.Assignment.Topic,
		ExtractionOK: true,
	}
	var outs []evaluator.CriterionResultOut
	maxBy := map[int64]float64{}
	for _, c := range ac.Assignment.Criteria {
		in.Criteria = append(in.Criteria, evaluator.CriterionInput{
			ID: c.ID, Title: c.Title, Description: c.Description,
			MaxPoints: float64(c.MaxPoints), Topic: c.Topic, Keywords: c.Keywords,
		})
		maxBy[c.ID] = float64(c.MaxPoints)
		dc, ok := ac.Draft.ByCriterion[c.ID]
		if !ok {
			return nil, fmt.Errorf("criterion %d (%s) is missing a score", c.ID, c.Title)
		}
		outs = append(outs, evaluator.CriterionResultOut{
			CriterionID: c.ID, AwardedPoints: dc.Points, Evidence: dc.Evidence, Feedback: dc.Feedback,
		})
	}
	for id := range ac.Draft.ByCriterion {
		if _, known := maxBy[id]; !known {
			return nil, fmt.Errorf("draft contains unknown criterion %d", id)
		}
	}
	res, err := evaluator.ValidateAgentResult(in, outs, args.Overall)
	if err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}
	res.Strengths = args.Strengths
	res.LearningGaps = args.Gaps
	res.Suggestions = args.Suggestions
	res.Confidence = strings.ToUpper(args.Confidence)
	res.ConfidenceReason = args.Reason
	if res.Confidence == "LOW" {
		res.NeedsReview = true
	}
	evid, err := ac.Store.PersistEvaluation(ac.Sub.ID, *res)
	if err != nil {
		return nil, fmt.Errorf("persist evaluation: %w", err)
	}
	return map[string]any{
		"saved":         true,
		"evaluation_id": evid,
		"total":         res.TotalScore,
		"status":        "NEEDS_REVIEW",
		"publish_state": "faculty approval required before students see marks",
	}, nil
}
