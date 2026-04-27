// Package checkpoint provides per-turn shadow git snapshots used by the
// Esc-Esc rewind feature. Snapshots live under refs/sam/checkpoints/<session>/<turn>
// and never touch the user's HEAD or working tree. All operations are pure
// Go (go-git/v5) — no shell-out to the git CLI.
package checkpoint

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// RefPrefix is the reserved namespace for sam checkpoint refs.
const RefPrefix = "refs/sam/checkpoints/"

// SessionID returns a fresh per-session identifier of the form
// `<unix-seconds>-<6-byte-hex>`.
func SessionID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%d-%s", time.Now().Unix(), hex.EncodeToString(b[:]))
}

// IsGitRepo reports whether path is inside a git repository (or is one).
func IsGitRepo(path string) bool {
	_, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{DetectDotGit: true})
	return err == nil
}

// Manager owns a session's checkpoint state. Safe for concurrent use.
type Manager struct {
	repoPath  string
	sessionID string
	repo      *git.Repository
	mu        sync.Mutex
}

// New opens the git repo containing repoPath and binds a Manager to sessionID.
func New(repoPath, sessionID string) (*Manager, error) {
	if sessionID == "" {
		return nil, errors.New("checkpoint: empty session id")
	}
	repo, err := git.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil, fmt.Errorf("checkpoint: open repo: %w", err)
	}
	return &Manager{repoPath: repoPath, sessionID: sessionID, repo: repo}, nil
}

// SessionID returns the session id this manager is bound to.
func (m *Manager) SessionID() string { return m.sessionID }

// refName returns the canonical ref name for the given turn.
func (m *Manager) refName(turn int) plumbing.ReferenceName {
	return plumbing.ReferenceName(RefPrefix + m.sessionID + "/" + strconv.Itoa(turn))
}

// Snapshot creates a tree+commit object capturing the current working tree
// (respecting .gitignore) and updates refs/sam/checkpoints/<session>/<turn>
// to point at that commit. The user's HEAD, current branch, and working tree
// are left exactly as they were.
//
// Returns the new commit SHA. If the repo has no HEAD yet (fresh repo with no
// commits), Snapshot returns an error — checkpoints require at least one
// existing commit so we have something to parent against.
func (m *Manager) Snapshot(turn int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	wt, err := m.repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("checkpoint: worktree: %w", err)
	}

	// Capture state we need to restore after the commit.
	origHeadSym, _ := m.repo.Reference(plumbing.HEAD, false)
	var origBranchRef *plumbing.Reference
	if origHeadSym != nil && origHeadSym.Type() == plumbing.SymbolicReference {
		if r, err := m.repo.Reference(origHeadSym.Target(), false); err == nil {
			origBranchRef = r
		}
	}

	origIdx, _ := m.repo.Storer.Index()
	var idxBackup *index.Index
	if origIdx != nil {
		cp := *origIdx
		cp.Entries = append([]*index.Entry(nil), origIdx.Entries...)
		cp.Cache = origIdx.Cache
		cp.ResolveUndo = origIdx.ResolveUndo
		cp.EndOfIndexEntry = origIdx.EndOfIndexEntry
		idxBackup = &cp
	}

	// Always restore HEAD branch and index, even on failure mid-flight.
	defer func() {
		if origBranchRef != nil {
			_ = m.repo.Storer.SetReference(origBranchRef)
		}
		if idxBackup != nil {
			_ = m.repo.Storer.SetIndex(idxBackup)
		}
	}()

	// Stage every tracked + untracked path that is not gitignored.
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		// AddWithOptions on an empty repo with no files is a no-op; we keep
		// going so AllowEmptyCommits below produces a stable empty snapshot.
		if !errors.Is(err, git.ErrEmptyCommit) {
			// Not fatal — log via error return only if Commit also fails.
		}
	}

	// Choose the parent: previous turn's snapshot (chained) when present,
	// otherwise the current HEAD commit.
	var parents []plumbing.Hash
	if turn > 1 {
		if r, err := m.repo.Reference(m.refName(turn-1), true); err == nil {
			parents = []plumbing.Hash{r.Hash()}
		}
	}
	if len(parents) == 0 && origBranchRef != nil {
		parents = []plumbing.Hash{origBranchRef.Hash()}
	}

	sig := &object.Signature{
		Name:  "sam",
		Email: "sam@checkpoint.local",
		When:  time.Now(),
	}
	commitHash, err := wt.Commit(fmt.Sprintf("sam: checkpoint turn %d", turn), &git.CommitOptions{
		Author:            sig,
		Committer:         sig,
		AllowEmptyCommits: true,
		Parents:           parents,
	})
	if err != nil {
		return "", fmt.Errorf("checkpoint: commit: %w", err)
	}

	ref := plumbing.NewHashReference(m.refName(turn), commitHash)
	if err := m.repo.Storer.SetReference(ref); err != nil {
		return "", fmt.Errorf("checkpoint: set ref: %w", err)
	}
	return commitHash.String(), nil
}

// SnapshotSHA returns the commit SHA stored at the given turn, or empty if
// the ref does not exist.
func (m *Manager) SnapshotSHA(turn int) string {
	r, err := m.repo.Reference(m.refName(turn), true)
	if err != nil {
		return ""
	}
	return r.Hash().String()
}

// Turns lists all turn numbers with snapshots for this session, ascending.
func (m *Manager) Turns() ([]int, error) {
	prefix := RefPrefix + m.sessionID + "/"
	iter, err := m.repo.References()
	if err != nil {
		return nil, err
	}
	var out []int
	defer iter.Close()
	err = iter.ForEach(func(r *plumbing.Reference) error {
		name := string(r.Name())
		if !strings.HasPrefix(name, prefix) {
			return nil
		}
		n, err := strconv.Atoi(strings.TrimPrefix(name, prefix))
		if err == nil {
			out = append(out, n)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Ints(out)
	return out, nil
}

// repoRoot returns the worktree root of the manager's repo.
func (m *Manager) repoRoot() (string, error) {
	wt, err := m.repo.Worktree()
	if err != nil {
		return "", err
	}
	return wt.Filesystem.Root(), nil
}

// listSessionRefs returns all session ids that currently have at least one
// checkpoint ref in repoPath.
func listSessionRefs(repo *git.Repository) (map[string]bool, error) {
	out := make(map[string]bool)
	iter, err := repo.References()
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	err = iter.ForEach(func(r *plumbing.Reference) error {
		name := string(r.Name())
		if !strings.HasPrefix(name, RefPrefix) {
			return nil
		}
		rest := strings.TrimPrefix(name, RefPrefix)
		// rest looks like "<session>/<turn>"
		slash := strings.IndexByte(rest, '/')
		if slash <= 0 {
			return nil
		}
		out[rest[:slash]] = true
		return nil
	})
	return out, err
}

// deleteSessionRefs removes every refs/sam/checkpoints/<session>/* ref from
// the supplied repo.
func deleteSessionRefs(repo *git.Repository, sessionID string) error {
	prefix := RefPrefix + sessionID + "/"
	iter, err := repo.References()
	if err != nil {
		return err
	}
	var names []plumbing.ReferenceName
	_ = iter.ForEach(func(r *plumbing.Reference) error {
		if strings.HasPrefix(string(r.Name()), prefix) {
			names = append(names, r.Name())
		}
		return nil
	})
	iter.Close()
	for _, n := range names {
		if err := repo.Storer.RemoveReference(n); err != nil {
			return err
		}
	}
	return nil
}

// SessionsStateDir returns $XDG_STATE_HOME/sam/sessions (or
// ~/.local/state/sam/sessions). Returns "" if it cannot be determined.
func SessionsStateDir() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "sam", "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "sam", "sessions")
}

// commitTree returns the tree object at the given turn's snapshot, or an
// error if no such ref exists / object is missing.
func (m *Manager) commitTree(turn int) (*object.Tree, error) {
	r, err := m.repo.Reference(m.refName(turn), true)
	if err != nil {
		return nil, err
	}
	c, err := m.repo.CommitObject(r.Hash())
	if err != nil {
		return nil, err
	}
	return c.Tree()
}

// MaxTurn returns the highest turn number with a snapshot for this session,
// or 0 if no snapshots exist.
func (m *Manager) MaxTurn() (int, error) {
	turns, err := m.Turns()
	if err != nil {
		return 0, err
	}
	if len(turns) == 0 {
		return 0, nil
	}
	return turns[len(turns)-1], nil
}

// ListPaths returns the union of paths whose content differs between the
// snapshot at turn and the latest snapshot. These are the paths the agent
// touched in turns (turn+1 .. maxTurn) and would therefore be candidates
// for surgical restore. Paths are repo-root-relative, sorted, deduplicated.
func (m *Manager) ListPaths(turn int) ([]string, error) {
	max, err := m.MaxTurn()
	if err != nil {
		return nil, err
	}
	if max == 0 || turn >= max {
		return nil, nil
	}
	from, err := m.commitTree(turn)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: list paths from turn %d: %w", turn, err)
	}
	to, err := m.commitTree(max)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: list paths to turn %d: %w", max, err)
	}
	changes, err := object.DiffTree(from, to)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(changes))
	var out []string
	for _, ch := range changes {
		// A change has From + To entries. Either side's name is meaningful;
		// take whichever is non-empty (renames count as both for our purposes).
		if n := ch.From.Name; n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
		if n := ch.To.Name; n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Restore writes paths from the snapshot at turn into the working tree.
// Files present in the snapshot are overwritten with their snapshot content;
// files absent from the snapshot are removed from the working tree (since
// they were created by the turns we're undoing).
//
// Restore does not touch HEAD, the user's branch ref, or the index; only
// the working tree files in `paths` are modified. Caller is responsible for
// honoring overlap warnings before calling.
func (m *Manager) Restore(turn int, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	root, err := m.repoRoot()
	if err != nil {
		return err
	}
	tree, err := m.commitTree(turn)
	if err != nil {
		return fmt.Errorf("checkpoint: restore turn %d: %w", turn, err)
	}
	for _, rel := range paths {
		clean := filepath.Clean(rel)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("checkpoint: refuse path outside repo: %q", rel)
		}
		abs := filepath.Join(root, clean)

		f, err := tree.File(rel)
		if err != nil {
			if errors.Is(err, object.ErrFileNotFound) {
				if rmErr := os.Remove(abs); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
					return fmt.Errorf("checkpoint: remove %s: %w", rel, rmErr)
				}
				continue
			}
			return fmt.Errorf("checkpoint: tree.File(%s): %w", rel, err)
		}
		// Submodules: refuse — restoring a gitlink entry would require nested
		// repo state we don't track. Skip silently rather than corrupt.
		if f.Mode == filemode.Submodule {
			continue
		}
		rd, err := f.Reader()
		if err != nil {
			return fmt.Errorf("checkpoint: read blob %s: %w", rel, err)
		}
		data, err := io.ReadAll(rd)
		rd.Close()
		if err != nil {
			return fmt.Errorf("checkpoint: drain blob %s: %w", rel, err)
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return fmt.Errorf("checkpoint: mkdir %s: %w", filepath.Dir(abs), err)
		}
		// Always Lremove first so we replace any existing symlink at abs
		// instead of writing through it (which would corrupt the link target).
		if err := os.Remove(abs); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("checkpoint: pre-remove %s: %w", rel, err)
		}
		if f.Mode == filemode.Symlink {
			if err := os.Symlink(string(data), abs); err != nil {
				return fmt.Errorf("checkpoint: symlink %s: %w", rel, err)
			}
			continue
		}
		mode := os.FileMode(0o644)
		if perm, err := f.Mode.ToOSFileMode(); err == nil {
			if p := perm.Perm(); p != 0 {
				mode = p
			}
		}
		if err := os.WriteFile(abs, data, mode); err != nil {
			return fmt.Errorf("checkpoint: write %s: %w", rel, err)
		}
	}
	return nil
}

// OverlapPaths returns the subset of paths whose current working-tree content
// hashes differently than the latest snapshot's blob. These represent edits
// the user made by hand (or via tools) AFTER the latest agent turn finished
// and would be silently clobbered by Restore. Caller should warn / require
// explicit confirmation before restoring overlapping paths.
//
// A path missing from the working tree but present in the latest snapshot is
// also considered overlapping (something deleted it).
func (m *Manager) OverlapPaths(paths []string) ([]string, error) {
	max, err := m.MaxTurn()
	if err != nil || max == 0 {
		return nil, err
	}
	tree, err := m.commitTree(max)
	if err != nil {
		return nil, err
	}
	root, err := m.repoRoot()
	if err != nil {
		return nil, err
	}
	var overlaps []string
	for _, rel := range paths {
		abs := filepath.Join(root, rel)

		var snapHash plumbing.Hash
		hadSnap := true
		f, ferr := tree.File(rel)
		if ferr != nil {
			if !errors.Is(ferr, object.ErrFileNotFound) {
				return nil, ferr
			}
			hadSnap = false
		} else {
			snapHash = f.Hash
		}

		data, rerr := os.ReadFile(abs)
		hadFS := rerr == nil
		if rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			return nil, rerr
		}

		switch {
		case !hadSnap && !hadFS:
			// Not in snapshot and not on disk — no overlap.
		case !hadSnap && hadFS:
			// File exists on disk but wasn't in latest snapshot:
			// user (or someone) created it after the last agent turn.
			overlaps = append(overlaps, rel)
		case hadSnap && !hadFS:
			// File deleted from working tree after last agent turn.
			overlaps = append(overlaps, rel)
		default:
			fsHash := plumbing.ComputeHash(plumbing.BlobObject, data)
			if fsHash != snapHash {
				overlaps = append(overlaps, rel)
			}
		}
	}
	sort.Strings(overlaps)
	return overlaps, nil
}

// DeleteSession removes every refs/sam/checkpoints/<sessionID>/* ref this
// manager owns. Used at clean shutdown so the per-session checkpoints don't
// accumulate forever.
func (m *Manager) DeleteSession() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return deleteSessionRefs(m.repo, m.sessionID)
}

// SetSnapshotRef writes refs/sam/checkpoints/<sid>/<turn> to point at the
// given commit hash. Used by rewind-undo to resurrect refs that were dropped
// by DeleteFromTurn — the underlying commit objects survive because go-git
// does not garbage-collect them within a single process lifetime.
func (m *Manager) SetSnapshotRef(turn int, sha string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := plumbing.NewHash(sha)
	if h.IsZero() {
		return fmt.Errorf("checkpoint: empty SHA for turn %d", turn)
	}
	ref := plumbing.NewHashReference(m.refName(turn), h)
	return m.repo.Storer.SetReference(ref)
}

// DeleteFromTurn removes every snapshot ref for this session whose turn
// number is strictly greater than keepThrough. Used by rewind to drop refs
// that point at undone work — without this, MaxTurn / ListPaths / OverlapPaths
// would keep returning stale data after the user rewinds.
func (m *Manager) DeleteFromTurn(keepThrough int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := RefPrefix + m.sessionID + "/"
	iter, err := m.repo.References()
	if err != nil {
		return err
	}
	var toRemove []plumbing.ReferenceName
	_ = iter.ForEach(func(r *plumbing.Reference) error {
		name := string(r.Name())
		if !strings.HasPrefix(name, prefix) {
			return nil
		}
		n, err := strconv.Atoi(strings.TrimPrefix(name, prefix))
		if err == nil && n > keepThrough {
			toRemove = append(toRemove, r.Name())
		}
		return nil
	})
	iter.Close()
	for _, n := range toRemove {
		if err := m.repo.Storer.RemoveReference(n); err != nil {
			return err
		}
	}
	return nil
}

// SweepOrphans removes refs/sam/checkpoints/<session>/* for any session id
// whose sidecar JSON is missing OR whose sidecar mtime is older than
// olderThan. This catches sessions where sam crashed or was killed without
// a clean DeleteSession.
//
// sidecarMTime is supplied by the caller (typically agent.SessionMTime) so
// this package stays free of the agent dependency. Pass nil to skip mtime
// checking and only sweep sessions whose sidecar is fully missing.
func SweepOrphans(repoPath string, olderThan time.Duration, sidecarMTime func(sessionID string) time.Time) error {
	repo, err := git.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return err
	}
	sessions, err := listSessionRefs(repo)
	if err != nil {
		return err
	}
	now := time.Now()
	for sid := range sessions {
		drop := false
		if sidecarMTime == nil {
			// Without an mtime probe we cannot tell live from orphan; skip.
			continue
		}
		mt := sidecarMTime(sid)
		switch {
		case mt.IsZero():
			drop = true
		case olderThan > 0 && now.Sub(mt) > olderThan:
			drop = true
		}
		if drop {
			if err := deleteSessionRefs(repo, sid); err != nil {
				return err
			}
		}
	}
	return nil
}

