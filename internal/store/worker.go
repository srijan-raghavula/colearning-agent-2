package store

import (
	"context"
	"log"
	"time"

	"educore/internal/evaluator"
)

// Worker drains the durable evaluation queue. One worker process at a time is
// supported; claims are transactional so restarts never duplicate evaluation.
type Worker struct {
	S *Store
	E *evaluator.Engine // nil Engine → DEMO mode
	// Notify wakes the worker immediately after a submission.
	Notify chan struct{}
	poll   time.Duration
}

// NewWorker builds a queue worker.
func NewWorker(s *Store, e *evaluator.Engine) *Worker {
	return &Worker{S: s, E: e, Notify: make(chan struct{}, 8), poll: 1500 * time.Millisecond}
}

// Run blocks until ctx ends; it recovers interrupted jobs first.
func (w *Worker) Run(ctx context.Context) {
	if n, err := w.S.RecoverStuckJobs(); err != nil {
		log.Printf("worker: recover jobs: %v", err)
	} else if n > 0 {
		log.Printf("worker: recovered %d interrupted evaluation job(s)", n)
	}
	t := time.NewTicker(w.poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-w.Notify:
		}
		w.drainOnce(ctx)
	}
}

func (w *Worker) drainOnce(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if !w.ProcessNext() {
			return
		}
	}
}

// ProcessNext claims and processes one queued job; false when the queue is
// empty. Exported so tests can force a deterministic drain.
func (w *Worker) ProcessNext() bool {
	jobID, err := w.S.ClaimNextJob()
	if err != nil {
		log.Printf("worker: claim: %v", err)
		return false
	}
	if jobID == 0 {
		return false
	}
	subID, err := w.S.JobSubmission(jobID)
	if err != nil {
		log.Printf("worker: resolve job %d: %v", jobID, err)
		return true
	}
	in, err := w.S.BuildEvaluatorInput(subID)
	if err != nil {
		w.S.FailJob(jobID, "cannot prepare evaluation input: "+err.Error())
		return true
	}
	res, err := w.E.Evaluate(context.Background(), in)
	if err != nil {
		// Provider/malformed failures keep the submission and mark FAILED for
		// faculty retry; no silent fallback to DEMO.
		w.S.FailJob(jobID, err.Error())
		return true
	}
	if _, err := w.S.PersistEvaluation(subID, *res); err != nil {
		w.S.FailJob(jobID, "persist evaluation: "+err.Error())
		return true
	}
	w.S.CompleteJob(jobID)
	return true
}
