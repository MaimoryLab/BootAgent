package process

import (
	"io/fs"

	"golang.org/x/sys/windows"
)

// liveFileState reports a file's size and write time as the file system holds
// them now.
//
// Directory enumeration is not enough on Windows: NTFS updates the size and
// write time in a directory entry lazily, so a file another process is still
// writing can be listed at its old size until that process closes it. A
// tarball holding one large binary would then look idle for its whole
// download. Querying an open handle reads the live values instead.
//
// The handle asks for attributes only and shares read, write and delete, so it
// cannot cause a sharing violation for the installer or stop it renaming or
// removing the file -- npm moves whole package directories during an update.
func liveFileState(path string, entry fs.DirEntry) (int64, int64, bool) {
	if entry.IsDir() {
		info, err := entry.Info()
		if err != nil {
			return 0, 0, false
		}
		return 0, info.ModTime().UnixNano(), true
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, false
	}
	handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return 0, 0, false
	}
	defer windows.CloseHandle(handle)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return 0, 0, false
	}
	size := int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow)
	return size, info.LastWriteTime.Nanoseconds(), true
}
