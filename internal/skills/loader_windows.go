//go:build windows

package skills

import "os"

func inode(fi os.FileInfo) (uint64, bool) { return 0, false }
