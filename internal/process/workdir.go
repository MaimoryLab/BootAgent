package process

import (
	"context"
	"path/filepath"
	"strings"
)

type workingDirectoryKey struct{}

// WithWorkingDirectory makes a command started under ctx run in directory
// instead of inheriting this process's own.
//
// It travels on the context for the same reason WithActivityPaths does: Runner
// stays the two-method interface the install tests assert against, and every
// decorator between the caller and the runner passes it along unchanged.
func WithWorkingDirectory(ctx context.Context, directory string) context.Context {
	if directory = strings.TrimSpace(directory); directory == "" {
		return ctx
	}
	return context.WithValue(ctx, workingDirectoryKey{}, filepath.Clean(directory))
}

// WorkingDirectory reports the directory WithWorkingDirectory set, or "".
func WorkingDirectory(ctx context.Context) string {
	directory, _ := ctx.Value(workingDirectoryKey{}).(string)
	return directory
}
