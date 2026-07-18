package metrics

import (
	"sync"
	"time"

	"server-shell-mcp/internal/app"
)

type Snapshot struct {
	Requests       int
	Failures       int
	Timeouts       int
	Truncated      int
	TotalDuration  time.Duration
	CurrentRunning int
	MaxRunning     int
}

type Recorder struct {
	mu       sync.Mutex
	snapshot Snapshot
}

func NewRecorder() *Recorder {
	return &Recorder{}
}

func (r *Recorder) Begin() func() {
	r.mu.Lock()
	r.snapshot.CurrentRunning++
	if r.snapshot.CurrentRunning > r.snapshot.MaxRunning {
		r.snapshot.MaxRunning = r.snapshot.CurrentRunning
	}
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.snapshot.CurrentRunning > 0 {
			r.snapshot.CurrentRunning--
		}
	}
}

func (r *Recorder) Record(result app.CommandResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshot.Requests++
	r.snapshot.TotalDuration += result.Duration
	switch result.Status {
	case app.StatusFailed, app.StatusRejected:
		r.snapshot.Failures++
	case app.StatusTimeout:
		r.snapshot.Timeouts++
	case app.StatusTruncated:
		r.snapshot.Truncated++
	}
}

func (r *Recorder) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshot
}
