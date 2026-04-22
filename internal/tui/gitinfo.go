package tui

import (
	"os/exec"
	"strings"
)

type gitInfo struct {
	branch string
	dirty  bool
}

func probeGit(dir string) gitInfo {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return gitInfo{}
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" {
		return gitInfo{}
	}
	status, _ := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	return gitInfo{branch: branch, dirty: len(strings.TrimSpace(string(status))) > 0}
}
