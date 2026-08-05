package app

import (
	"testing"
	"time"
)

func TestTaskLockSerializesOneTarget(t *testing.T) {
	core := &UseCases{}
	firstUnlock := core.lockTask("download:node")

	acquired := make(chan struct{})
	done := make(chan struct{})
	go func() {
		unlock := core.lockTask("download:node")
		close(acquired)
		unlock()
		close(done)
	}()

	select {
	case <-acquired:
		t.Fatal("the same target lock was acquired concurrently")
	case <-time.After(20 * time.Millisecond):
	}

	firstUnlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the waiting target lock was not released")
	}
}
