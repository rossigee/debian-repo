package reconcile

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestJobManagerSingleFlight(t *testing.T) {
	mgr := NewJobManager()

	// Start first job
	id1, started1 := mgr.TryStart()
	if !started1 {
		t.Error("expected first TryStart to succeed")
	}

	// Try to start second job while first is running
	id2, started2 := mgr.TryStart()
	if started2 {
		t.Error("expected second TryStart to fail while first is running")
	}

	// Should return the same job ID
	if id1 != id2 {
		t.Errorf("expected same job ID, got %q and %q", id1, id2)
	}

	// Complete the first job
	mgr.Complete(id1, nil, 0)

	// Now should be able to start a new job
	id3, started3 := mgr.TryStart()
	if !started3 {
		t.Error("expected TryStart to succeed after completing first job")
	}

	if id1 == id3 {
		t.Error("expected different job ID after new TryStart")
	}
}

func TestJobManagerGetNotFound(t *testing.T) {
	mgr := NewJobManager()
	_, ok := mgr.Get("nonexistent")
	if ok {
		t.Error("expected Get to return false for nonexistent job")
	}
}

func TestJobManagerComplete(t *testing.T) {
	mgr := NewJobManager()
	id, _ := mgr.TryStart()

	discrepancies := []Discrepancy{
		{Kind: "test", Message: "test message"},
	}
	mgr.Complete(id, discrepancies, 123)

	job, ok := mgr.Get(id)
	if !ok {
		t.Fatal("expected Get to return true for completed job")
	}

	if job.Status != JobStatusComplete {
		t.Errorf("expected status Complete, got %v", job.Status)
	}

	if len(job.Discrepancies) != 1 {
		t.Errorf("expected 1 discrepancy, got %d", len(job.Discrepancies))
	}

	if job.SnapshotGen != 123 {
		t.Errorf("expected snapshot gen 123, got %d", job.SnapshotGen)
	}

	if job.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

func TestJobManagerFail(t *testing.T) {
	mgr := NewJobManager()
	id, _ := mgr.TryStart()

	err := fmt.Errorf("test error")
	mgr.Fail(id, err)

	job, ok := mgr.Get(id)
	if !ok {
		t.Fatal("expected Get to return true for failed job")
	}

	if job.Status != JobStatusFailed {
		t.Errorf("expected status Failed, got %v", job.Status)
	}

	if job.Error != "test error" {
		t.Errorf("expected error message 'test error', got %q", job.Error)
	}

	if job.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

func TestJobManagerUpdateProgress(t *testing.T) {
	mgr := NewJobManager()
	id, _ := mgr.TryStart()

	mgr.UpdateProgress(id, 50, 100)

	job, _ := mgr.Get(id)
	if job.Processed != 50 || job.Total != 100 {
		t.Errorf("expected progress 50/100, got %d/%d", job.Processed, job.Total)
	}
}

func TestJobManagerPruning(t *testing.T) {
	mgr := NewJobManager()

	// Create more than maxStored jobs
	for i := 0; i < 25; i++ {
		id, _ := mgr.TryStart()
		mgr.Complete(id, nil, 0)
		time.Sleep(1 * time.Millisecond) // Ensure different timestamps
	}

	// Should only have maxStored jobs
	if len(mgr.jobs) > mgr.maxStored {
		t.Errorf("expected at most %d jobs, got %d", mgr.maxStored, len(mgr.jobs))
	}
}

func TestJobManagerCopyPreventRaces(t *testing.T) {
	mgr := NewJobManager()
	id, _ := mgr.TryStart()

	discrepancies := []Discrepancy{
		{Kind: "test1", Message: "msg1"},
	}
	mgr.Complete(id, discrepancies, 0)

	job1, _ := mgr.Get(id)
	job1.Discrepancies = append(job1.Discrepancies, Discrepancy{Kind: "test2", Message: "msg2"})

	job2, _ := mgr.Get(id)
	if len(job2.Discrepancies) != 1 {
		t.Errorf("expected original to have 1 discrepancy, got %d", len(job2.Discrepancies))
	}
}

func TestJobManagerConcurrentAccess(t *testing.T) {
	mgr := NewJobManager()
	var wg sync.WaitGroup

	// Multiple goroutines trying to start jobs
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, started := mgr.TryStart()
			if started {
				time.Sleep(10 * time.Millisecond)
				mgr.UpdateProgress(id, 5, 10)
				mgr.Complete(id, nil, 0)
			} else {
				// Just verify we got a job ID back
				if id == "" {
					t.Error("expected non-empty job ID even when not started")
				}
			}
		}()
	}

	wg.Wait()
}
