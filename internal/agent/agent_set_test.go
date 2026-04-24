package agent

import (
	"log/slog"
	"testing"
)

func TestSetSystem_UpdatesBaseAndEffective(t *testing.T) {
	a := &Agent{baseSystem: "old", system: "old", log: slog.Default()}
	a.SetSystem("new")
	if a.baseSystem != "new" {
		t.Fatalf("baseSystem: %q", a.baseSystem)
	}
	if a.system != "new" {
		t.Fatalf("effective system: %q", a.system)
	}
}

func TestSetSystem_ConcurrentSafe(t *testing.T) {
	a := &Agent{log: slog.Default()}
	done := make(chan struct{}, 2)
	go func() {
		for i := 0; i < 1000; i++ {
			a.SetSystem("a")
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 1000; i++ {
			a.SetSystem("b")
		}
		done <- struct{}{}
	}()
	<-done
	<-done
	if a.baseSystem == "" {
		t.Fatal("empty baseSystem after concurrent writes")
	}
}
