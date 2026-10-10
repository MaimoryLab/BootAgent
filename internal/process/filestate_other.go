//go:build !windows

package process

import "io/fs"

// liveFileState reports a file's size and write time. Outside Windows the
// directory entry's metadata is the inode's, so it is current even while
// another process is still writing the file.
func liveFileState(_ string, entry fs.DirEntry) (int64, int64, bool) {
	info, err := entry.Info()
	if err != nil {
		return 0, 0, false
	}
	return info.Size(), info.ModTime().UnixNano(), true
}
