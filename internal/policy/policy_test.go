package policy

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestDefaultPolicy(t *testing.T) {
	p := Default()

	// Read should be Allow
	if d := p.Check("Read", nil); d != Allow {
		t.Errorf("Read: expected Allow, got %v", d)
	}

	// Write should be Ask
	if d := p.Check("Write", nil); d != Ask {
		t.Errorf("Write: expected Ask, got %v", d)
	}

	// Edit should be Ask
	if d := p.Check("Edit", nil); d != Ask {
		t.Errorf("Edit: expected Ask, got %v", d)
	}

	// Bash should be Ask
	if d := p.Check("Bash", nil); d != Ask {
		t.Errorf("Bash: expected Ask, got %v", d)
	}

	// Unknown tool should be Ask
	if d := p.Check("Unknown", nil); d != Ask {
		t.Errorf("Unknown: expected Ask, got %v", d)
	}
}

func TestAllowAllPolicy(t *testing.T) {
	p := AllowAll()

	// All tools should be Allow
	if d := p.Check("Read", nil); d != Allow {
		t.Errorf("Read: expected Allow, got %v", d)
	}
	if d := p.Check("Write", nil); d != Allow {
		t.Errorf("Write: expected Allow, got %v", d)
	}
	if d := p.Check("Edit", nil); d != Allow {
		t.Errorf("Edit: expected Allow, got %v", d)
	}
	if d := p.Check("Bash", nil); d != Allow {
		t.Errorf("Bash: expected Allow, got %v", d)
	}
}

func TestAllowSession(t *testing.T) {
	p := Default()

	// Initially should be Ask
	if d := p.Check("Bash", nil); d != Ask {
		t.Errorf("Bash: expected Ask, got %v", d)
	}

	// Allow for session
	p.AllowSession("Bash")

	// Now should be Allow
	if d := p.Check("Bash", nil); d != Allow {
		t.Errorf("Bash after AllowSession: expected Allow, got %v", d)
	}

	// Other tools should still be Ask
	if d := p.Check("Write", nil); d != Ask {
		t.Errorf("Write: expected Ask, got %v", d)
	}
}

func TestConcurrentAccess(t *testing.T) {
	p := Default()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Check("Read", nil)
			p.AllowSession("Bash")
		}()
	}
	wg.Wait()
}

func TestCheckWithInput(t *testing.T) {
	p := Default()
	input := json.RawMessage(`{"file_path": "/tmp/test"}`)

	// Should not panic with input
	d := p.Check("Read", input)
	if d != Allow {
		t.Errorf("Read with input: expected Allow, got %v", d)
	}
}
