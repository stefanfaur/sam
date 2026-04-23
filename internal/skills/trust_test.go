package skills

import "testing"

func TestTrustList_State(t *testing.T) {
	tl := NewTrustList(
		map[string]bool{"/Users/a/repo": true},
		map[string]bool{"/Users/a/evil": true},
	)
	if tl.State("/Users/a/repo") != TrustAllowed {
		t.Errorf("repo should be allowed")
	}
	if tl.State("/Users/a/evil") != TrustDenied {
		t.Errorf("evil should be denied")
	}
	if tl.State("/Users/a/unknown") != TrustPending {
		t.Errorf("unknown should be pending")
	}
	if tl.State("") != TrustPending {
		t.Errorf("empty should be pending")
	}
}

func TestTrustList_Normalization(t *testing.T) {
	tl := NewTrustList(map[string]bool{"/Users/a/repo/": true}, nil)
	if tl.State("/Users/a/repo") != TrustAllowed {
		t.Errorf("trailing slash should normalize")
	}
	if tl.State("/Users/a/repo/") != TrustAllowed {
		t.Errorf("with trailing slash should also resolve")
	}
}

func TestTrustList_Transitions(t *testing.T) {
	tl := NewTrustList(nil, nil)
	tl.Trust("/p")
	if tl.State("/p") != TrustAllowed {
		t.Errorf("after Trust should be allowed")
	}
	tl.Deny("/p")
	if tl.State("/p") != TrustDenied {
		t.Errorf("Deny should move from trusted to denied")
	}
	if tl.Trusted["/p"] {
		t.Errorf("Deny should remove from trusted")
	}
	tl.Clear("/p")
	if tl.State("/p") != TrustPending {
		t.Errorf("after Clear should be pending")
	}
}
