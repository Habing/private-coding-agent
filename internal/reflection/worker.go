package reflection

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	pcametrics "github.com/yourorg/private-coding-agent/internal/metrics"
)

// processor is the subset of Reflector the worker calls. Defined as an
// interface so worker_test can drop in a fake.
type processor interface {
	Reflect(ctx context.Context, job ReflectionJob) error
}

// jobStore persists reflection jobs across process restarts.
type jobStore interface {
	Enqueue(ctx context.Context, tenantID, userID, sessionID uuid.UUID, maxAttempts int) (uuid.UUID, bool, error)
	ListDue(ctx context.Context, limit int) ([]JobRow, error)
	MarkProcessing(ctx context.Context, id uuid.UUID) error
	MarkCompleted(ctx context.Context, id uuid.UUID) error
	MarkFailed(ctx context.Context, id uuid.UUID, err error, retryBase time.Duration) error
	ExpireStalePendingProposals(ctx context.Context, ttlDays int) (int64, error)
}

// Worker drains ReflectionJob from a buffered channel into the processor.
// When a jobStore is wired, jobs are persisted before enqueue and replayed
// after restart. Best-effort: when the channel is full, Enqueue drops the
// in-memory handoff but the DB row remains for the poll loop.
type Worker struct {
	proc       processor
	store      jobStore
	jobs       chan ReflectionJob
	timeout    time.Duration
	pollEvery  time.Duration
	maxAttempts int
	retryBase  time.Duration
	proposalTTLDays int

	done   chan struct{}
	closed chan struct{}
}

// WorkerOptions tunes durable-queue behaviour. Zero values pick sensible
// defaults inside NewWorker.
type WorkerOptions struct {
	Store             jobStore
	MaxAttempts       int
	RetryBaseInterval time.Duration
	PollInterval      time.Duration
	ProposalPendingTTLDays int
}

// NewWorker builds a worker with the given buffer and per-job timeout.
// buffer ≤ 0 defaults to 256; timeout ≤ 0 defaults to 5min.
func NewWorker(proc processor, buffer int, timeout time.Duration, opts WorkerOptions) *Worker {
	if buffer <= 0 {
		buffer = 256
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	retryBase := opts.RetryBaseInterval
	if retryBase <= 0 {
		retryBase = time.Minute
	}
	pollEvery := opts.PollInterval
	if pollEvery <= 0 {
		pollEvery = 30 * time.Second
	}
	return &Worker{
		proc:            proc,
		store:           opts.Store,
		jobs:            make(chan ReflectionJob, buffer),
		timeout:         timeout,
		pollEvery:       pollEvery,
		maxAttempts:     maxAttempts,
		retryBase:       retryBase,
		proposalTTLDays: opts.ProposalPendingTTLDays,
		done:            make(chan struct{}),
		closed:          make(chan struct{}),
	}
}

// Enqueue is the hook session.Service calls after ArchiveSession. Returns true
// if the job was accepted (persisted and/or queued). Never blocks the caller.
func (w *Worker) Enqueue(ctx context.Context, tenantID, userID, sessionID uuid.UUID) bool {
	job := ReflectionJob{TenantID: tenantID, UserID: userID, SessionID: sessionID}
	if w.store != nil {
		id, created, err := w.store.Enqueue(ctx, tenantID, userID, sessionID, w.maxAttempts)
		if err != nil {
			slog.Warn("reflection: persist job failed",
				"tenant_id", tenantID, "session_id", sessionID, "err", err.Error())
			w.bumpDropped()
			return false
		}
		if !created {
			// Already queued or completed for this session.
			return true
		}
		job.JobID = id
	}
	if w.tryPush(job) {
		return true
	}
	if w.store != nil {
		// Durable row remains; poll loop will pick it up.
		slog.Warn("reflection: channel full; job persisted for later poll",
			"tenant_id", tenantID, "session_id", sessionID)
		return true
	}
	w.bumpDropped()
	return false
}

func (w *Worker) tryPush(job ReflectionJob) bool {
	select {
	case <-w.done:
		return false
	default:
	}
	select {
	case w.jobs <- job:
		return true
	default:
		return false
	}
}

// Run consumes jobs until ctx is cancelled or Stop is called.
func (w *Worker) Run(ctx context.Context) {
	defer close(w.closed)
	w.pollDue(ctx)
	poll := time.NewTicker(w.pollEvery)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			w.shutdown()
			return
		case <-w.done:
			w.shutdown()
			return
		case <-poll.C:
			w.pollDue(ctx)
			w.expireProposals(ctx)
		case job := <-w.jobs:
			w.handle(job)
		}
	}
}

// Stop signals the worker to stop accepting new jobs and drains the queue.
func (w *Worker) Stop() {
	select {
	case <-w.done:
	default:
		close(w.done)
	}
	<-w.closed
}

func (w *Worker) pollDue(ctx context.Context) {
	if w.store == nil {
		return
	}
	rows, err := w.store.ListDue(ctx, 32)
	if err != nil {
		slog.Warn("reflection: list due jobs", "err", err.Error())
		return
	}
	for _, row := range rows {
		job := ReflectionJob{
			JobID:     row.ID,
			TenantID:  row.TenantID,
			UserID:    row.UserID,
			SessionID: row.SessionID,
		}
		if !w.tryPush(job) {
			return
		}
	}
}

func (w *Worker) expireProposals(ctx context.Context) {
	if w.store == nil || w.proposalTTLDays <= 0 {
		return
	}
	n, err := w.store.ExpireStalePendingProposals(ctx, w.proposalTTLDays)
	if err != nil {
		slog.Warn("reflection: expire stale proposals", "err", err.Error())
		return
	}
	if n > 0 {
		slog.Info("reflection: expired stale pending proposals", "count", n)
	}
}

func (w *Worker) handle(job ReflectionJob) {
	if w.store != nil && job.JobID != uuid.Nil {
		if err := w.store.MarkProcessing(context.Background(), job.JobID); err != nil {
			slog.Warn("reflection: mark processing", "job_id", job.JobID, "err", err.Error())
			return
		}
	}
	jobCtx, cancel := context.WithTimeout(context.Background(), w.timeout)
	defer cancel()
	err := w.proc.Reflect(jobCtx, job)
	if err != nil {
		if w.store != nil && job.JobID != uuid.Nil {
			if ferr := w.store.MarkFailed(context.Background(), job.JobID, err, w.retryBase); ferr != nil {
				slog.Warn("reflection: mark failed", "job_id", job.JobID, "err", ferr.Error())
			}
		}
		slog.Warn("reflection: reflect failed",
			"tenant_id", job.TenantID, "session_id", job.SessionID, "err", err.Error())
		return
	}
	if w.store != nil && job.JobID != uuid.Nil {
		if err := w.store.MarkCompleted(context.Background(), job.JobID); err != nil {
			slog.Warn("reflection: mark completed", "job_id", job.JobID, "err", err.Error())
		}
	}
}

func (w *Worker) shutdown() {
	for {
		select {
		case job := <-w.jobs:
			w.handle(job)
		default:
			return
		}
	}
}

func (w *Worker) bumpDropped() {
	if pcametrics.ReflectionProposalsTotal == nil {
		return
	}
	pcametrics.ReflectionProposalsTotal.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("outcome", "dropped"),
	))
}

// EnqueueFunc is the function-type signature the session.Service hook expects.
type EnqueueFunc func(ctx context.Context, tenantID, userID, sessionID uuid.UUID) bool
