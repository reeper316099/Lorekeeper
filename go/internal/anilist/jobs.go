package anilist

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

type JobState string

const (
	JobRunning   JobState = "running"
	JobDone      JobState = "done"
	JobCancelled JobState = "cancelled"
	JobError     JobState = "error"
	JobOffline   JobState = "offline"
)

// Job is a long-running AniList operation ("match" or "import"). Snapshots are what the API returns.
type Job struct {
	Done      int      `json:"done"`
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Message   *string  `json:"message"`
	Result    any      `json:"result,omitempty"`
	StartedAt string   `json:"startedAt"`
	State     JobState `json:"state"`
	Total     int      `json:"total"`
}

type jobEntry struct {
	job    Job
	cancel context.CancelFunc
}

// JobRegistry keeps AniList jobs in memory, with cancellation and progress callbacks.
type JobRegistry struct {
	mu       sync.Mutex
	jobs     map[string]*jobEntry
	order    []string
	onChange func(Job)
}

func NewJobRegistry(onChange func(Job)) *JobRegistry {
	return &JobRegistry{jobs: map[string]*jobEntry{}, onChange: onChange}
}

// Create registers a running job and returns its id and a context that is cancelled by Cancel.
func (r *JobRegistry) Create(kind string, total int) (id string, ctx context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	j := Job{ID: uuid.NewString(), Kind: kind, State: JobRunning, Total: total, StartedAt: store.ISO(time.Now())}
	r.mu.Lock()
	r.jobs[j.ID] = &jobEntry{job: j, cancel: cancel}
	r.order = append(r.order, j.ID)
	r.mu.Unlock()
	r.notify(j)
	return j.ID, ctx
}

func (r *JobRegistry) notify(j Job) {
	if r.onChange != nil {
		r.onChange(j)
	}
}

// Get returns a snapshot of a job.
func (r *JobRegistry) Get(id string) (Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.jobs[id]; ok {
		return e.job, true
	}
	return Job{}, false
}

// Update changes a job and notifies listeners; it returns the new snapshot.
func (r *JobRegistry) Update(id string, patch func(*Job)) Job {
	r.mu.Lock()
	e := r.jobs[id]
	if e == nil {
		r.mu.Unlock()
		return Job{}
	}
	patch(&e.job)
	j := e.job
	r.mu.Unlock()
	r.notify(j)
	return j
}

// Cancel asks a running job to stop; false when it is unknown or already finished.
func (r *JobRegistry) Cancel(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.jobs[id]
	if e == nil || e.job.State != JobRunning {
		return false
	}
	e.cancel()
	return true
}

// Running is the newest running job of a kind, if any (the UI re-attaches to it after a reload).
func (r *JobRegistry) Running(kind string) *Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.order) - 1; i >= 0; i-- {
		if e := r.jobs[r.order[i]]; e.job.Kind == kind && e.job.State == JobRunning {
			j := e.job
			return &j
		}
	}
	return nil
}
