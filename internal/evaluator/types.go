// Package evaluator implements the automated assessment engine: input types,
// strict validation of model output, evidence checking, OpenAI-compatible
// provider adapter and the deterministic DEMO evaluator.
package evaluator

import "fmt"

// Confidence levels.
const (
	ConfHigh   = "HIGH"
	ConfMedium = "MEDIUM"
	ConfLow    = "LOW"
)

// Evaluation methods.
const (
	MethodDemo   = "DEMO"
	MethodAI     = "AI"
	MethodManual = "MANUAL"
)

// CriterionInput is one faculty-defined rubric criterion handed to the engine.
type CriterionInput struct {
	ID          int64
	Title       string
	Description string
	MaxPoints   float64
	Topic       string
	Keywords    string // comma-separated hints
}

// Input is a full evaluation request.
type Input struct {
	AssignmentTitle string
	Instructions    string
	Topic           string
	CourseName      string
	SubmissionText  string
	MaxTextBytes    int // safety cap applied by caller
	AssignmentID    int64
	SubmissionID    int64
	Criteria        []CriterionInput
	ExtractionOK    bool // false when document text extraction failed
}

// Evidence is one quotation with justification.
type Evidence struct {
	Quote       string `json:"quote"`
	Explanation string `json:"explanation"`
}

// CriterionResultOut is the model's per-criterion proposal before validation.
// The evaluator validates points/evidence and produces CriterionResult.
type CriterionResultOut struct {
	CriterionID   int64      `json:"criterion_id"`
	AwardedPoints float64    `json:"awarded_points"`
	Evidence      []Evidence `json:"evidence"`
	Feedback      string     `json:"feedback"`
}

// CriterionResult is a validated per-criterion result ready for persistence.
type CriterionResult struct {
	CriterionID     int64
	AwardedPoints   float64
	Evidence        []Evidence // only verified evidence kept
	DroppedEvidence []Evidence // quotations that failed verification
	Feedback        string
}

// Warning records a validation/evidence problem for faculty visibility.
type Warning struct {
	Level     string `json:"level"` // "warning" | "info" | "error"
	Message   string `json:"message"`
	Criterion string `json:"criterion,omitempty"`
}

// Result is the final validated evaluation output.
type Result struct {
	TotalScore       float64           // computed here, never from the model
	Criteria         []CriterionResult `json:"criteria"`
	OverallFeedback  string            `json:"overall_feedback"`
	Strengths        []string          `json:"strengths"`
	LearningGaps     []string          `json:"learning_gaps"`
	Suggestions      []string          `json:"suggested_improvements"`
	Confidence       string            `json:"confidence"`
	ConfidenceReason string            `json:"confidence_reason"`
	Method           string            // DEMO | AI | MANUAL
	Warnings         []Warning         `json:"warnings"`
	NeedsReview      bool              // low confidence / evidence problems / extraction failure
}

// parseError wraps malformed model output.
type parseError struct{ msg string }

func (e parseError) Error() string { return e.msg }

// errResultInvalid marks structurally invalid model output.
var errResultInvalid = parseError{"invalid evaluation result"}

// totalForHelper is a comment-only helper marker.
var _ = fmt.Sprintf
