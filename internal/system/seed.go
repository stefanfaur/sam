package system

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const manifestName = ".seed-manifest.json"

type manifest struct {
	Version string            `json:"version"`
	Hashes  map[string]string `json:"hashes"`
}

func newManifest() manifest {
	return manifest{Version: "1", Hashes: map[string]string{}}
}

// Seed writes embedded defaults into dir, respecting manifest-tracked edits.
// Idempotent. Atomic per-file writes. Returns first error encountered; files
// already written are kept.
func Seed(dir string, logger *slog.Logger) error {
	if err := os.MkdirAll(filepath.Join(dir, "tools"), 0755); err != nil {
		return err
	}

	m := loadManifest(dir)
	if m.Hashes == nil {
		m.Hashes = map[string]string{}
	}
	m.Version = "1"

	var firstErr error
	err := fs.WalkDir(defaultsFS, "defaults", func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel("defaults", path)
		if err != nil {
			return err
		}
		// Normalize to forward slashes for manifest keys.
		relKey := filepath.ToSlash(rel)

		data, err := defaultsFS.ReadFile(path)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		embeddedHash := sha256Hex(data)

		tgt := filepath.Join(dir, filepath.FromSlash(rel))
		existing, statErr := os.ReadFile(tgt)
		if statErr != nil {
			if !errors.Is(statErr, fs.ErrNotExist) {
				if firstErr == nil {
					firstErr = statErr
				}
				return nil
			}
			// Missing — write embedded.
			if werr := atomicWrite(tgt, data, 0644); werr != nil {
				if firstErr == nil {
					firstErr = werr
				}
				return nil
			}
			m.Hashes[relKey] = embeddedHash
			return nil
		}

		diskHash := sha256Hex(existing)
		recorded, recordedOK := m.Hashes[relKey]

		if recordedOK && diskHash == recorded {
			// User has not edited — safe to overwrite with new embedded.
			if diskHash == embeddedHash {
				// No change, just keep manifest hash.
				m.Hashes[relKey] = embeddedHash
				return nil
			}
			if werr := atomicWrite(tgt, data, 0644); werr != nil {
				if firstErr == nil {
					firstErr = werr
				}
				return nil
			}
			m.Hashes[relKey] = embeddedHash
			return nil
		}

		// Disk differs from manifest → treat as user-edited. Preserve.
		if recordedOK && recorded != embeddedHash && logger != nil {
			logger.Info("system: new default available", "file", relKey)
		}
		// Keep recorded hash if present; otherwise record current disk hash so
		// next upgrade has a stable baseline.
		if !recordedOK {
			m.Hashes[relKey] = diskHash
		}
		return nil
	})
	if err != nil && firstErr == nil {
		firstErr = err
	}

	if werr := saveManifest(dir, m); werr != nil && firstErr == nil {
		firstErr = werr
	}
	return firstErr
}

// LoadSystemPrompt reads dir/system-prompt.md; returns "" if missing.
func LoadSystemPrompt(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "system-prompt.md"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return string(b), nil
}

// LoadToolDescription reads dir/tools/<lower-name>.md; returns "" if missing.
// Trailing whitespace trimmed for parity with embedded descriptions.
func LoadToolDescription(dir, toolName string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "tools", toolFile(toolName)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimRight(string(b), "\n\r\t "), nil
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// atomicWrite writes data to path via a unique tmp sibling + rename so
// concurrent writers do not clobber each other's tmp files.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// loadManifest returns an empty manifest if missing or corrupt.
func loadManifest(dir string) manifest {
	b, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return newManifest()
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return newManifest()
	}
	if m.Hashes == nil {
		m.Hashes = map[string]string{}
	}
	if m.Version == "" {
		m.Version = "1"
	}
	return m
}

func saveManifest(dir string, m manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, manifestName), b, 0644)
}
