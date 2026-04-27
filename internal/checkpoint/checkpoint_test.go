package checkpoint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestSessionID_FormatAndUniqueness(t *testing.T) {
	a := SessionID()
	b := SessionID()
	if a == b {
		t.Fatalf("session ids should be unique: %s", a)
	}
	for _, id := range []string{a, b} {
		dash := strings.IndexByte(id, '-')
		if dash <= 0 {
			t.Fatalf("session id missing dash: %s", id)
		}
		secs := id[:dash]
		hex := id[dash+1:]
		if len(hex) != 12 {
			t.Fatalf("session id hex part wrong length: %s", id)
		}
		if _, err := time.Parse("2006", "2024"); err != nil {
			t.Skip()
		}
		if secs == "0" || secs == "" {
			t.Fatalf("session id seconds invalid: %s", id)
		}
	}
}

func TestIsGitRepo(t *testing.T) {
	tmp := t.TempDir()
	if IsGitRepo(tmp) {
		t.Fatalf("empty dir should not be a git repo")
	}
	if _, err := git.PlainInit(tmp, false); err != nil {
		t.Fatalf("init: %v", err)
	}
	if !IsGitRepo(tmp) {
		t.Fatalf("init'd dir should be a git repo")
	}
	sub := filepath.Join(tmp, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if !IsGitRepo(sub) {
		t.Fatalf("subdir of git repo should be detected (DetectDotGit)")
	}
}

// initRepoWithCommit creates a temp repo with one initial commit, returns root.
func initRepoWithCommit(t *testing.T) (string, *git.Repository) {
	t.Helper()
	tmp := t.TempDir()
	repo, err := git.PlainInit(tmp, false)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	if _, err := wt.Add("a.txt"); err != nil {
		t.Fatal(err)
	}
	sig := &object.Signature{Name: "t", Email: "t@t", When: time.Now()}
	if _, err := wt.Commit("init", &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatal(err)
	}
	return tmp, repo
}

func TestSnapshot_CreatesRefAndPreservesHEAD(t *testing.T) {
	root, repo := initRepoWithCommit(t)

	headBefore, err := repo.Reference(plumbing.HEAD, true)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	branchBefore, err := repo.Head()
	if err != nil {
		t.Fatalf("branch: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr, err := New(root, "sess-test-1")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	sha, err := mgr.Snapshot(1)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(sha) != 40 {
		t.Fatalf("expected 40-char SHA, got %q", sha)
	}

	// Working tree intact.
	if data, _ := os.ReadFile(filepath.Join(root, "b.txt")); string(data) != "two\n" {
		t.Fatalf("working tree changed: %q", data)
	}

	// HEAD ref unchanged (still points at original branch SHA).
	headAfter, err := repo.Reference(plumbing.HEAD, true)
	if err != nil {
		t.Fatalf("head after: %v", err)
	}
	if headAfter.Hash() != headBefore.Hash() {
		t.Fatalf("HEAD moved: before=%s after=%s", headBefore.Hash(), headAfter.Hash())
	}
	branchAfter, err := repo.Head()
	if err != nil {
		t.Fatalf("branch after: %v", err)
	}
	if branchAfter.Hash() != branchBefore.Hash() {
		t.Fatalf("current branch ref moved: before=%s after=%s", branchBefore.Hash(), branchAfter.Hash())
	}

	// Shadow ref written.
	ref, err := repo.Reference(plumbing.ReferenceName(RefPrefix+"sess-test-1/1"), true)
	if err != nil {
		t.Fatalf("shadow ref missing: %v", err)
	}
	if ref.Hash().String() != sha {
		t.Fatalf("shadow ref mismatch")
	}

	// Snapshot's tree must contain the new file.
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	tree, _ := commit.Tree()
	if _, err := tree.File("b.txt"); err != nil {
		t.Fatalf("snapshot tree missing b.txt: %v", err)
	}
}

func TestSnapshot_ChainsParents(t *testing.T) {
	root, repo := initRepoWithCommit(t)
	mgr, _ := New(root, "sess-chain")

	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha1, err := mgr.Snapshot(1)
	if err != nil {
		t.Fatalf("snap1: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "c.txt"), []byte("three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha2, err := mgr.Snapshot(2)
	if err != nil {
		t.Fatalf("snap2: %v", err)
	}
	if sha2 == sha1 {
		t.Fatalf("snapshots collided")
	}

	c2, err := repo.CommitObject(plumbing.NewHash(sha2))
	if err != nil {
		t.Fatalf("commit2: %v", err)
	}
	if c2.NumParents() != 1 {
		t.Fatalf("snap2 parents = %d, want 1", c2.NumParents())
	}
	parent, _ := c2.Parent(0)
	if parent.Hash.String() != sha1 {
		t.Fatalf("snap2 parent = %s, want %s", parent.Hash, sha1)
	}
}

func TestTurnsListing(t *testing.T) {
	root, _ := initRepoWithCommit(t)
	mgr, _ := New(root, "sess-list")

	for _, n := range []int{1, 2, 3} {
		if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte{byte('0' + n)}, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := mgr.Snapshot(n); err != nil {
			t.Fatalf("snap %d: %v", n, err)
		}
	}
	turns, err := mgr.Turns()
	if err != nil {
		t.Fatalf("turns: %v", err)
	}
	if len(turns) != 3 || turns[0] != 1 || turns[2] != 3 {
		t.Fatalf("turns = %v", turns)
	}
}

func TestSessionsStateDir_RespectsXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-test")
	if got := SessionsStateDir(); got != "/tmp/xdg-test/sam/sessions" {
		t.Fatalf("got %q", got)
	}
}

func TestRestore_RewritesSnapshotPaths(t *testing.T) {
	root, _ := initRepoWithCommit(t)
	mgr, _ := New(root, "sess-restore")

	// Turn 1 snapshot includes b.txt = "two"
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Snapshot(1); err != nil {
		t.Fatal(err)
	}

	// Turn 2 snapshot mutates b.txt and adds c.txt
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("two-edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "c.txt"), []byte("three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Snapshot(2); err != nil {
		t.Fatal(err)
	}

	// Restore turn 1 — both paths.
	paths := []string{"b.txt", "c.txt"}
	if err := mgr.Restore(1, paths); err != nil {
		t.Fatalf("restore: %v", err)
	}

	// b.txt should be "two\n" again (original turn-1 content).
	if data, _ := os.ReadFile(filepath.Join(root, "b.txt")); string(data) != "two\n" {
		t.Fatalf("b.txt = %q", data)
	}
	// c.txt was created in turn 2 → not present in turn 1 → must be deleted.
	if _, err := os.Stat(filepath.Join(root, "c.txt")); !os.IsNotExist(err) {
		t.Fatalf("c.txt should be deleted, got err=%v", err)
	}
}

func TestRestore_RefusesEscapedPaths(t *testing.T) {
	root, _ := initRepoWithCommit(t)
	mgr, _ := New(root, "sess-escape")
	if _, err := mgr.Snapshot(1); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Restore(1, []string{"../etc/passwd"}); err == nil {
		t.Fatal("expected refusal on escape path")
	}
	if err := mgr.Restore(1, []string{"/etc/passwd"}); err == nil {
		t.Fatal("expected refusal on absolute path")
	}
}

func TestListPaths_DifferingBetweenSnapshots(t *testing.T) {
	root, _ := initRepoWithCommit(t)
	mgr, _ := New(root, "sess-list-paths")

	if err := os.WriteFile(filepath.Join(root, "x.txt"), []byte("x1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Snapshot(1); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "x.txt"), []byte("x2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "y.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Snapshot(2); err != nil {
		t.Fatal(err)
	}

	paths, err := mgr.ListPaths(1)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"x.txt": true, "y.txt": true}
	if len(paths) != 2 {
		t.Fatalf("paths = %v", paths)
	}
	for _, p := range paths {
		if !want[p] {
			t.Errorf("unexpected path %q", p)
		}
	}
}

func TestOverlapPaths_FlagsUserEdits(t *testing.T) {
	root, _ := initRepoWithCommit(t)
	mgr, _ := New(root, "sess-overlap")

	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("b1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Snapshot(1); err != nil {
		t.Fatal(err)
	}

	// User edits a.txt outside the agent.
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("v1-USER-EDIT"), 0o644); err != nil {
		t.Fatal(err)
	}

	overlaps, err := mgr.OverlapPaths([]string{"a.txt", "b.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(overlaps) != 1 || overlaps[0] != "a.txt" {
		t.Fatalf("overlaps = %v", overlaps)
	}
}

func TestOverlapPaths_DeletedFile(t *testing.T) {
	root, _ := initRepoWithCommit(t)
	mgr, _ := New(root, "sess-overlap-del")
	if err := os.WriteFile(filepath.Join(root, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Snapshot(1); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "x.txt")); err != nil {
		t.Fatal(err)
	}
	overlaps, err := mgr.OverlapPaths([]string{"x.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(overlaps) != 1 {
		t.Fatalf("expected x.txt flagged: %v", overlaps)
	}
}

func TestDeleteSession_RemovesAllRefs(t *testing.T) {
	root, repo := initRepoWithCommit(t)
	mgr, _ := New(root, "sess-delete")
	for n := 1; n <= 3; n++ {
		if _, err := mgr.Snapshot(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := mgr.DeleteSession(); err != nil {
		t.Fatal(err)
	}
	iter, _ := repo.References()
	defer iter.Close()
	count := 0
	_ = iter.ForEach(func(r *plumbing.Reference) error {
		if strings.HasPrefix(string(r.Name()), RefPrefix+"sess-delete/") {
			count++
		}
		return nil
	})
	if count != 0 {
		t.Fatalf("refs remained: %d", count)
	}
}

func TestDeleteFromTurn_RemovesOnlyHigherTurns(t *testing.T) {
	root, repo := initRepoWithCommit(t)
	mgr, _ := New(root, "sess-trim")
	for n := 1; n <= 4; n++ {
		mgr.Snapshot(n)
	}
	if err := mgr.DeleteFromTurn(2); err != nil {
		t.Fatal(err)
	}
	turns, _ := mgr.Turns()
	if len(turns) != 2 || turns[0] != 1 || turns[1] != 2 {
		t.Fatalf("turns = %v, want [1 2]", turns)
	}
	// Resurrect via SetSnapshotRef using a saved SHA → ref reappears.
	c1 := mgr.SnapshotSHA(1)
	if err := mgr.SetSnapshotRef(3, c1); err != nil {
		t.Fatal(err)
	}
	if got := mgr.SnapshotSHA(3); got != c1 {
		t.Fatalf("SnapshotSHA after SetSnapshotRef = %q", got)
	}
	_ = repo
}

func TestRestore_SymlinkRoundTrip(t *testing.T) {
	root, _ := initRepoWithCommit(t)
	if err := os.Symlink("a.txt", filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	mgr, _ := New(root, "sess-symlink")
	if _, err := mgr.Snapshot(1); err != nil {
		t.Fatal(err)
	}
	// User replaces the symlink with a regular file (something the rewind
	// would want to undo). Restore must turn it back into a symlink, not
	// write through the (now-absent) link target.
	os.Remove(filepath.Join(root, "link"))
	os.WriteFile(filepath.Join(root, "link"), []byte("bogus"), 0o644)
	if err := mgr.Restore(1, []string{"link"}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Lstat(filepath.Join(root, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link is not a symlink: mode=%v", st.Mode())
	}
	target, _ := os.Readlink(filepath.Join(root, "link"))
	if target != "a.txt" {
		t.Fatalf("link target = %q, want a.txt", target)
	}
}

func TestSweepOrphans_DropsMissingSidecar(t *testing.T) {
	root, repo := initRepoWithCommit(t)
	mgrA, _ := New(root, "live")
	mgrB, _ := New(root, "orphan")
	mgrA.Snapshot(1)
	mgrB.Snapshot(1)

	mtime := func(sid string) time.Time {
		if sid == "live" {
			return time.Now()
		}
		return time.Time{} // missing
	}
	if err := SweepOrphans(root, 7*24*time.Hour, mtime); err != nil {
		t.Fatal(err)
	}

	iter, _ := repo.References()
	defer iter.Close()
	live, orphan := 0, 0
	_ = iter.ForEach(func(r *plumbing.Reference) error {
		n := string(r.Name())
		if strings.HasPrefix(n, RefPrefix+"live/") {
			live++
		}
		if strings.HasPrefix(n, RefPrefix+"orphan/") {
			orphan++
		}
		return nil
	})
	if live != 1 || orphan != 0 {
		t.Fatalf("after sweep: live=%d orphan=%d", live, orphan)
	}
}
