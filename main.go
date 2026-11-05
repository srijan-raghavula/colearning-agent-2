// Command educore is the EDUCORE academic assessment intelligence platform.
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"educore/internal/auth"
	"educore/internal/config"
	"educore/internal/database"
	"educore/internal/evaluator"
	"educore/internal/handlers"
	"educore/internal/middleware"
	"educore/internal/seed"
	"educore/internal/store"
)

func main() {
	cfg := config.Load()

	// Subcommand: `educore seed` — idempotent demo data.
	if len(os.Args) > 1 && os.Args[1] == "seed" {
		db, err := database.Open(cfg.DBPath)
		if err != nil {
			log.Fatalf("open database: %v", err)
		}
		defer database.Close(db)
		if err := seed.Run(db); err != nil {
			log.Fatalf("seed: %v", err)
		}
		log.Printf("seed complete (%s)", database.Now())
		return
	}

	// Subcommand: `educore provider-check` — exactly one tiny DeepInfra call,
	// validates connectivity + key, then exits. Uses ≤64 output tokens.
	if len(os.Args) > 1 && os.Args[1] == "provider-check" {
		providerCheck(cfg)
		return
	}

	// Subcommand: `educore evaluate-once --submission=ID --confirm` — one real
	// end-to-end AI evaluation of the given submission (single request, one
	// retry budget), persisted as NEEDS_REVIEW; never auto-approves.
	if len(os.Args) > 1 && os.Args[1] == "evaluate-once" {
		evaluateOnce(cfg, os.Args[2:])
		return
	}

	log.Printf("educore starting: %s", cfg)

	db, err := database.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer database.Close(db)

	// Idempotent seed on a fresh database only.
	var userCount int
	if err := db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&userCount); err != nil {
		log.Fatalf("probe users: %v", err)
	}
	if userCount == 0 {
		if err := seed.Run(db); err != nil {
			log.Fatalf("seed: %v", err)
		}
	}

	// Evaluation engine: real provider when credentials exist, DEMO otherwise.
	st := store.New(db)
	var engine *evaluator.Engine
	var learningAI handlers.LearningAIProvider
	if cfg.AIEnabled() {
		provider := &evaluator.Provider{BaseURL: cfg.AIBaseURL, APIKey: cfg.AIKey, Model: cfg.AIModel}
		engine = &evaluator.Engine{Provider: provider, MaxTokens: cfg.AIMaxTokens}
		if strings.TrimSpace(cfg.AIBaseURL) != "" {
			learningAI = provider
		}
		log.Printf("evaluation mode: AI (%s)", cfg.AIModel)
	} else {
		engine = &evaluator.Engine{}
		log.Printf("evaluation mode: DEMO (deterministic rubric-keyword matching)")
	}
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	worker := store.NewWorker(st, engine)
	go worker.Run(workerCtx)
	agentEngine := startAgentEngine(cfg, st, cfg.AIEnabled())

	authSvc := auth.NewService(db)
	tpl, err := handlers.LoadTemplates()
	if err != nil {
		log.Fatalf("templates: %v", err)
	}

	deps := &handlers.Deps{
		Auth:         authSvc,
		CSRF:         middleware.CSRF{Secret: csrfSecret(db)},
		Tpl:          tpl,
		AIOn:         cfg.AIEnabled(),
		LearningAI:   learningAI,
		Store:        st,
		WorkerNotify: worker.Notify,
		AgentEngine:  agentEngine,
	}

	// Static assets + authenticated app routes.
	mux := http.NewServeMux()
	fileServer := http.FileServer(neuteredFS{http.Dir("static")})
	mux.Handle("GET /static/", http.StripPrefix("/static/", fileServer))
	mux.Handle("/", deps.Handler())

	// CSRF protection is applied per state-changing route inside Routes();
	// a global wrapper would also intercept the anonymous login POST, whose
	// token mechanism differs from session-bound tokens.
	handler := middleware.Auth{Service: authSvc}.WithSession(mux)

	stop := make(chan struct{})
	go middleware.PurgeSessions(authSvc, stop)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		close(stop)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	log.Printf("listening on http://%s", cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errorWasShutdown(err) {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("educore stopped cleanly")
}

func errorWasShutdown(err error) bool {
	return err == http.ErrServerClosed || strings.Contains(err.Error(), "Server closed")
}

// neuteredFS disables directory listings on the static file server.
type neuteredFS struct{ fs http.FileSystem }

func (n neuteredFS) Open(name string) (http.File, error) {
	f, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	st, serr := f.Stat()
	if serr != nil || st.IsDir() {
		return nil, os.ErrNotExist
	}
	return f, nil
}

// csrfSecret returns a stable per-database CSRF secret, creating one if needed.
func csrfSecret(db *sql.DB) string {
	var secret string
	err := db.QueryRow(`SELECT value FROM db_meta WHERE key = 'csrf_secret'`).Scan(&secret)
	if err == nil && len(secret) >= 32 {
		return secret
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// Unrecoverable entropy failure: fail closed at startup.
		log.Fatalf("generate csrf secret: %v", err)
	}
	secret = hex.EncodeToString(buf)
	_, err = db.Exec(`INSERT INTO db_meta (key, value) VALUES ('csrf_secret', ?)
	                  ON CONFLICT(key) DO UPDATE SET value = excluded.value`, secret)
	if err != nil {
		log.Fatalf("store csrf secret: %v", err)
	}
	return secret
}
