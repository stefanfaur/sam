//go:build darwin || linux || freebsd || netbsd || openbsd

package skills

import (
	"os"
	"syscall"
)

func inode(fi os.FileInfo) (uint64, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Ino), true
}
