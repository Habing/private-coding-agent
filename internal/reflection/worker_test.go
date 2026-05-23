package reflection_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/reflection"
)

type fakeProc struct {
	mu    sync.Mutex
	jobs  []reflection.ReflectionJob
	block chan struct{} // when non-nil, Reflect blocks on this
	calls int32
}

func (f *fakeProc) Reflect(ctx context.Context, j reflection.ReflectionJob) error {
	atomic.AddInt32(&f.calls, 1)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
		}
	}
	f.mu.Lock()
	f.jobs = append(f.jobs, j)
	f.mu.Unlock()
	return nil
}

func (f *fakeProc) Jobs() []reflection.ReflectionJob {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]reflection.ReflectionJob, len(f.jobs))
	copy(out, f.jobs)
	return out
}

func TestWorker_HappyPath_EnqueueAndProcess(t *testing.T) {
	fp := &fakeProc{}
	w := reflection.NewWorker(fp, 4, time.Second, reflection.WorkerOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	defer w.Stop()

	tid := uuid.New()
	uid := uuid.New()
	sid := uuid.New()
	require.True(t, w.Enqueue(context.Background(), tid, uid, sid))

	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&fp.calls) == 1
	}, time.Second, 10*time.Millisecond)
	jobs := fp.Jobs()
	require.Len(t, jobs, 1)
	require.Equal(t, sid, jobs[0].SessionID)
}

func TestWorker_FullChannel_DropsAndDoesNotBlock(t *testing.T) {
	block := make(chan struct{})
	fp := &fakeProc{block: block}
	w := reflection.NewWorker(fp, 1, time.Second, reflection.WorkerOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	defer func() {
		close(block) // release the in-flight job before Stop drains.
		w.Stop()
	}()

	tid := uuid.New()
	uid := uuid.New()

	// First Enqueue → consumed by worker goroutine (blocks on `block`).
	require.True(t, w.Enqueue(context.Background(), tid, uid, uuid.New()))
	// Give the worker a moment to pull it off the channel.
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&fp.calls) == 1
	}, time.Second, 10*time.Millisecond)
	// Second Enqueue → fills the buffer of size 1.
	require.True(t, w.Enqueue(context.Background(), tid, uid, uuid.New()))
	// Third Enqueue → must be dropped (buffer full, worker still blocked).
	enqDone := make(chan bool, 1)
	go func() { enqDone <- w.Enqueue(context.Background(), tid, uid, uuid.New()) }()
	select {
	case ok := <-enqDone:
		require.False(t, ok, "expected drop=false but got accepted")
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("Enqueue blocked instead of dropping")
	}
}

func TestWorker_StopDrainsBuffered(t *testing.T) {
	fp := &fakeProc{}
	w := reflection.NewWorker(fp, 4, time.Second, reflection.WorkerOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Pre-fill before Run starts.
	for i := 0; i < 3; i++ {
		require.True(t, w.Enqueue(context.Background(), uuid.New(), uuid.New(), uuid.New()))
	}
	go w.Run(ctx)
	w.Stop()

	require.Equal(t, int32(3), atomic.LoadInt32(&fp.calls))
	require.Len(t, fp.Jobs(), 3)
}

func TestWorker_AfterStop_EnqueueDrops(t *testing.T) {
	fp := &fakeProc{}
	w := reflection.NewWorker(fp, 4, time.Second, reflection.WorkerOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	w.Stop()

	require.False(t, w.Enqueue(context.Background(), uuid.New(), uuid.New(), uuid.New()))
}
