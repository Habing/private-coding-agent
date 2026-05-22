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

// Worker drains ReflectionJob from a buffered channel into the processor.
// Best-effort: when the channel is full, Enqueue drops the job and counts it.
// On Stop, the worker drains in-flight jobs up to the per-job timeout.
type Worker struct {
	proc    processor
	jobs    chan ReflectionJob
	timeout time.Duration
	done    chan struct{}
	closed  chan struct{}
}

// NewWorker builds a worker with the given buffer and per-job timeout.
// buffer ≤ 0 defaults to 256; timeout ≤ 0 defaults to 5min.
func NewWorker(proc processor, buffer int, timeout time.Duration) *Worker {
	if buffer <= 0 {
		buffer = 256
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &Worker{
		proc:    proc,
		jobs:    make(chan ReflectionJob, buffer),
		timeout: timeout,
		done:    make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

// Enqueue is the hook session.Service calls after ArchiveSession. Returns true
// if the job was queued, false if dropped (channel full or worker stopped).
// Never blocks the caller.
func (w *Worker) Enqueue(_ context.Context, tenantID, userID, sessionID uuid.UUID) bool {
	job := ReflectionJob{TenantID: tenantID, UserID: userID, SessionID: sessionID}
	select {
	case <-w.done:
		w.bumpDropped()
		return false
	default:
	}
	select {
	case w.jobs <- job:
		return true
	default:
		w.bumpDropped()
		slog.Warn("reflection: enqueue dropped job",
			"tenant_id", tenantID, "session_id", sessionID, "reason", "channel_full")
		return false
	}
}

// Run consumes jobs until ctx is cancelled or Stop is called. Each job runs
// in its own goroutine with a fresh timeout context derived from
// context.Background() so request-cancellation does not abort reflection.
func (w *Worker) Run(ctx context.Context) {
	defer close(w.closed)
	for {
		select {
		case <-ctx.Done():
			w.shutdown()
			return
		case <-w.done:
			w.shutdown()
			return
		case job := <-w.jobs:
			w.handle(job)
		}
	}
}

// Stop signals the worker to stop accepting new jobs and drains the queue.
// Blocks until Run returns. Safe to call multiple times.
func (w *Worker) Stop() {
	select {
	case <-w.done:
		// already stopped
	default:
		close(w.done)
	}
	<-w.closed
}

func (w *Worker) handle(job ReflectionJob) {
	jobCtx, cancel := context.WithTimeout(context.Background(), w.timeout)
	defer cancel()
	if err := w.proc.Reflect(jobCtx, job); err != nil {
		slog.Warn("reflection: reflect failed",
			"tenant_id", job.TenantID, "session_id", job.SessionID, "err", err.Error())
	}
}

// shutdown drains the remaining jobs in the channel synchronously so they
// are not lost on graceful stop.
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
