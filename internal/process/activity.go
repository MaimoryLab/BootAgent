package process

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
)

// maxActivityEntries bounds one path's share of a sample. The directories a
// caller names are an install's own output and cache staging area, a few
// thousand entries at most; the cap only guards a mistaken path that points
// somewhere enormous. It is per path, so one large tree cannot stop the others
// from being sampled -- the cache staging area is usually listed last and is
// the signal that matters most. The walk order is lexical, so a capped sample
// still compares consistently.
const maxActivityEntries = 20000

type activityPathsKey struct{}

// WithActivityPaths names directories whose growth counts as a command being
// alive, alongside its output.
//
// Output alone cannot tell a slow download from a hung one. npm prints nothing
// on a non-TTY while it fetches a tarball -- measured: zero bytes of output for
// the whole of a slow global install, while the package directory and npm's
// cache staging area grew the entire time. Claude Code's Windows binary is a
// 111 MB tarball, so a slow link spent minutes in that silence, past the stall
// timeout, and a download that was progressing was treated as hung.
//
// The paths travel on the context rather than through Runner so every decorator
// between the caller and the runner passes them along unchanged.
func WithActivityPaths(ctx context.Context, paths ...string) context.Context {
	cleaned := make([]string, 0, len(paths))
	for _, path := range paths {
		if path = strings.TrimSpace(path); path != "" {
			cleaned = append(cleaned, filepath.Clean(path))
		}
	}
	if len(cleaned) == 0 {
		return ctx
	}
	return context.WithValue(ctx, activityPathsKey{}, append(activityPaths(ctx), cleaned...))
}

func activityPaths(ctx context.Context) []string {
	paths, _ := ctx.Value(activityPathsKey{}).([]string)
	return append([]string(nil), paths...)
}

// pathSignature summarises the watched trees. Any change between two samples is
// progress: a new entry, a file that grew, or a newer write time. A missing path
// contributes nothing, so a directory that only appears mid-install is itself a
// change.
type pathSignature struct {
	entries int
	bytes   int64
	newest  int64
}

func samplePaths(paths []string, done <-chan struct{}) pathSignature {
	var signature pathSignature
	for _, root := range paths {
		walked := 0
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			select {
			case <-done:
				return filepath.SkipAll
			default:
			}
			if err != nil {
				// A file renamed or removed between listing and stat is ordinary
				// while npm moves packages around; skip it, keep walking.
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if walked >= maxActivityEntries {
				return filepath.SkipAll
			}
			walked++
			signature.entries++
			if entry.Type()&fs.ModeSymlink != 0 {
				// Never followed: a link out of the tree could reach anything.
				return nil
			}
			if size, modified, ok := liveFileState(path, entry); ok {
				signature.bytes += size
				signature.newest = max(signature.newest, modified)
			}
			return nil
		})
	}
	return signature
}
