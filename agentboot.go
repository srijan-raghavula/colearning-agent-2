package main

import (
	"context"
	"log"

	"educore/internal/agent"
	"educore/internal/config"
	"educore/internal/evaluator"
	"educore/internal/store"
)

// DefaultAgentDuration is the total wall-clock budget per agent run (0 = unlimited).
const DefaultAgentDuration = 0

// startAgentEngine runs the agent drainer alongside the main server (or off).
func startAgentEngine(cfg config.Config, st *store.Store, aiOn bool) *agent.Engine {
	var p *evaluator.Provider
	if aiOn {
		p = &evaluator.Provider{BaseURL: cfg.AIBaseURL, APIKey: cfg.AIKey, Model: cfg.AIModel}
		log.Printf("agent engine: AI model %s", cfg.AIModel)
	} else {
		log.Printf("agent engine: no AI credentials — runs will fail fast until AI_* is configured")
	}
	eng := agent.NewEngine(st, p, cfg.AIMaxTokens, DefaultAgentDuration)
	ctx := context.Background()
	go eng.Run(ctx)
	return eng
}
