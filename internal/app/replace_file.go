//go:build !windows

package app

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
