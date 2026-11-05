package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"educore/internal/config"
	"educore/internal/database"
	"educore/internal/evaluator"
	"educore/internal/store"
)

// providerCheck makes exactly ONE tiny provider request and reports status.
// The API key is never printed; only a masked form is shown.
func providerCheck(cfg config.Config) {
	if !cfg.AIEnabled() {
		fmt.Println("provider-check requires AI_BASE_URL, AI_API_KEY and AI_MODEL to be configured.")
		fmt.Println("DeepInfra example:\n  export AI_BASE_URL=https://api.deepinfra.com/v1/openai\n  export AI_API_KEY=<your key>\n  export AI_MODEL=<model id, e.g. zai-org/GLM-4.6>")
		os.Exit(1)
	}
	p := &evaluator.Provider{BaseURL: cfg.AIBaseURL, APIKey: cfg.AIKey, Model: cfg.AIModel}
	fmt.Printf("endpoint : %s/chat/completions\n", p.BaseURL)
	fmt.Printf("model    : %s\n", p.Model)
	fmt.Printf("api key  : %s (masked — never logged)\n", evaluator.SafeMask(cfg.AIKey))
	fmt.Printf("budget   : <=64 output tokens for this probe\n")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := evaluator.Probe(ctx, p, 32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "PROBE FAILED: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("status   : OK\nreply    : %s\noutput tokens allowed: %d\n", res.Raw, res.OutputTokens)
	fmt.Println("text generation connectivity confirmed. No key was printed.")

	// Second, SEPARATE verification: the tool-calling contract. This cannot be
	// inferred from text generation and is what the agent loop depends on.
	tres, terr := evaluator.ProbeToolCalling(ctx, p, 32)
	if terr != nil {
		fmt.Fprintf(os.Stderr, "TOOL PROBE FAILED: %v\n", terr)
		fmt.Fprintln(os.Stderr, "text generation works, but the agent loop REQUIRES the tool-calling contract — do not enable agent runs until this passes.")
		os.Exit(2)
	}
	fmt.Printf("tools    : finish_reason=%s tool_calls=%d %v\n", tres.FinishReason, tres.ToolCalls, tres.ToolNames)
	fmt.Println("tool-calling contract confirmed (assistant message + tool_calls wire shape parsed by EDUCORE).")
}

// evaluateOnce runs one real AI evaluation of a single submission through the
// production engine (with its one-retry budget), persists the provisional
// result, and never auto-approves.
func evaluateOnce(cfg config.Config, args []string) {
	if !cfg.AIEnabled() {
		fmt.Println("evaluate-once requires AI credentials (AI_BASE_URL, AI_API_KEY, AI_MODEL). The server DEMO mode remains available by default; this CLI runs real-provider evaluation only.")
		os.Exit(1)
	}
	fs := flag.NewFlagSet("evaluate-once", flag.ContinueOnError)
	submissionID := fs.Int64("submission", 0, "submission id to evaluate")
	confirm := fs.Bool("confirm", false, "explicit consent: run one real API evaluation")
	_ = fs.Parse(args)
	if !*confirm {
		fmt.Println("Refusing to call the provider without --confirm (API credits are limited).")
		os.Exit(1)
	}
	if *submissionID <= 0 {
		fmt.Println("usage: educore evaluate-once --submission=<id> --confirm")
		os.Exit(1)
	}
	db, err := database.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close(db)
	st := store.New(db)

	var status string
	if err := db.QueryRow(`SELECT COALESCE(eval_status,'') FROM submissions WHERE id = ?`, *submissionID).Scan(&status); err != nil {
		fmt.Println("submission not found:", *submissionID)
		os.Exit(1)
	}
	if status == store.EvalApproved {
		fmt.Println("submission is already approved; refusing to re-evaluate an approved record without explicit consent (use the faculty UI re-evaluate instead).")
		os.Exit(1)
	}
	in, err := st.BuildEvaluatorInput(*submissionID)
	if err != nil {
		fmt.Println("cannot build evaluation input:", err)
		os.Exit(1)
	}
	engine := &evaluator.Engine{
		Provider:  &evaluator.Provider{BaseURL: cfg.AIBaseURL, APIKey: cfg.AIKey, Model: cfg.AIModel},
		MaxTokens: cfg.AIMaxTokens,
	}
	fmt.Printf("endpoint : %s/chat/completions\n", cfg.AIBaseURL)
	fmt.Printf("model    : %s\n", cfg.AIModel)
	fmt.Printf("api key  : %s (masked)\n", evaluator.SafeMask(cfg.AIKey))
	fmt.Printf("output tokens cap: %d (per attempt; at most one retry for transient failures)\n", engine.MaxTokens)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	result, err := engine.Evaluate(ctx, in)
	if err != nil {
		// Keep the submission intact; never fabricate a demo/falsely
		// approved evaluation after a real-provider failure.
		_ = st.FailEval(*submissionID, err.Error())
		fmt.Fprintf(os.Stderr, "EVALUATION FAILED: %v\n", err)
		fmt.Println("Status set to FAILED; the submission content is unchanged (available for faculty retry or manual grading).")
		os.Exit(1)
	}
	id, err := st.PersistEvaluation(*submissionID, *result)
	if err != nil {
		fmt.Println("persist failed (result discarded):", err)
		os.Exit(1)
	}
	fmt.Printf("persisted evaluation #%d: attempt recorded, method=%s, confidence=%s, total=%f (server-calculated)\n",
		id, result.Method, result.Confidence, result.TotalScore)
	fmt.Println("state: NEEDS_REVIEW — faculty approval required before students see marks.")
}
