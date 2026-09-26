package reconcile

import (
	"fmt"
	"sync"
	"time"
)

// JobStatus represents the state of a reconciliation job.
type JobStatus string

const (
	// JobStatusRunning indicates the job is currently executing.
	JobStatusRunning JobStatus = "running"
	// JobStatusComplete indicates the job finished successfully.
	JobStatusComplete JobStatus = "complete"
	// JobStatusFailed indicates the job failed.
	JobStatusFailed JobStatus = "failed"
)

// JobState holds the state of a reconciliation job.
type JobState struct {
	ID            string
	Status        JobStatus
	StartedAt     time.Time
	CompletedAt   *time.Time
	Processed     int
	Total         int
	Discrepancies []Discrepancy
	Error         string
	SnapshotGen   int64
}

// JobManager tracks the state of reconciliation jobs and enforces single-flight semantics.
type JobManager struct {
	mu        sync.Mutex
	jobs      map[string]*JobState
	activeJob string
	maxStored int
}

// NewJobManager creates a new job manager with a retention cap of 20 completed/failed jobs.
func NewJobManager() *JobManager {
	return &JobManager{
		jobs:      make(map[string]*JobState),
		maxStored: 20,
	}
}

// TryStart returns a new job ID and true if started, or the currently-running job's ID and false.
func (m *JobManager) TryStart() (jobID string, started bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeJob != "" {
		return m.activeJob, false
	}

	jobID = fmt.Sprintf("recon-%d", time.Now().UnixNano())
	m.jobs[jobID] = &JobState{
		ID:        jobID,
		Status:    JobStatusRunning,
		StartedAt: time.Now(),
	}
	m.activeJob = jobID
	return jobID, true
}

// UpdateProgress updates the processed/total counts for a running job.
func (m *JobManager) UpdateProgress(id string, processed, total int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if job, ok := m.jobs[id]; ok {
		job.Processed = processed
		job.Total = total
	}
}

// Complete marks a job as finished with results.
func (m *JobManager) Complete(id string, discrepancies []Discrepancy, snapshotGen int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if job, ok := m.jobs[id]; ok {
		now := time.Now()
		job.Status = JobStatusComplete
		job.CompletedAt = &now
		job.Discrepancies = discrepancies
		job.SnapshotGen = snapshotGen
	}
	if m.activeJob == id {
		m.activeJob = ""
	}
	m.pruneOld()
}

// Fail marks a job as failed with an error message.
func (m *JobManager) Fail(id string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if job, ok := m.jobs[id]; ok {
		now := time.Now()
		job.Status = JobStatusFailed
		job.CompletedAt = &now
		job.Error = err.Error()
	}
	if m.activeJob == id {
		m.activeJob = ""
	}
	m.pruneOld()
}

// Get returns a copy of the job state (safe to read without holding the lock).
func (m *JobManager) Get(id string) (JobState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[id]
	if !ok {
		return JobState{}, false
	}

	// Return a copy to avoid races
	jobCopy := *job
	if job.CompletedAt != nil {
		t := *job.CompletedAt
		jobCopy.CompletedAt = &t
	}
	if job.Discrepancies != nil {
		jobCopy.Discrepancies = append([]Discrepancy{}, job.Discrepancies...)
	}
	return jobCopy, true
}

// pruneOld removes old completed/failed jobs keeping only maxStored most recent.
// Must be called with lock held.
func (m *JobManager) pruneOld() {
	if len(m.jobs) <= m.maxStored {
		return
	}

	// Find oldest completed/failed job and delete it
	var oldestID string
	var oldestTime time.Time

	for id, job := range m.jobs {
		if job.Status == JobStatusRunning {
			continue // Never prune active jobs
		}
		if oldestID == "" || job.CompletedAt.Before(oldestTime) {
			oldestID = id
			oldestTime = *job.CompletedAt
		}
	}

	if oldestID != "" {
		delete(m.jobs, oldestID)
	}
}
