package agent

import (
	"sync"
	"testing"
)

func TestQueueSteer(t *testing.T) {
	a := New(Options{MaxIters: 1})

	if len(a.GetQueue()) != 0 {
		t.Fatalf("expected empty queue, got %d items", len(a.GetQueue()))
	}

	a.QueueSteer("add unit tests")
	q := a.GetQueue()
	if len(q) != 1 || q[0] != "add unit tests" {
		t.Fatalf("expected queue [\"add unit tests\"], got %v", q)
	}

	a.QueueSteer("handle edge cases")
	q = a.GetQueue()
	if len(q) != 2 || q[1] != "handle edge cases" {
		t.Fatalf("expected queue length 2, got %d", len(q))
	}

	a.DiscardQueue()
	if len(a.GetQueue()) != 0 {
		t.Fatalf("expected empty queue after Discard, got %d items", len(a.GetQueue()))
	}
}

func TestQueueSteerConcurrency(t *testing.T) {
	a := New(Options{})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.QueueSteer("msg")
		}()
	}
	wg.Wait()

	if got := len(a.GetQueue()); got != 10 {
		t.Fatalf("expected queue length 10, got %d", got)
	}
}

func TestDrainQueue(t *testing.T) {
	a := New(Options{})
	a.QueueSteer("a")
	a.QueueSteer("b")

	drained := a.drainQueue()
	if len(drained) != 2 || drained[0] != "a" || drained[1] != "b" {
		t.Fatalf("drainQueue returned %v", drained)
	}
	if got := len(a.GetQueue()); got != 0 {
		t.Fatalf("expected empty queue after drain, got %d", got)
	}
}
