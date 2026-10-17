//go:build unix

package eksinventory

import (
	"io/fs"
	"os"
	"syscall"
)

func privateBaselineParent(info fs.FileInfo) bool {
	stat, hasStat := info.Sys().(*syscall.Stat_t)

	uid := os.Geteuid()
	if uid < 0 {
		return false
	}

	return hasStat && info.IsDir() && info.Mode().Perm()&0o077 == 0 &&
		uint64(stat.Uid) == uint64(uid)
}

func privateEvidenceFile(info fs.FileInfo) bool {
	stat, hasStat := info.Sys().(*syscall.Stat_t)
	uid := os.Geteuid()

	return hasStat && info.Mode().IsRegular() && info.Mode().Perm() == baselineFileMode &&
		uid >= 0 && uint64(stat.Uid) == uint64(uid)
}
