package config

import "os"

// DelayReadsForTest widens the window between a config being read and the merged
// result being written, and returns a function that restores the normal read.
//
// Exported because the operation that has to be serialised lives in the app layer,
// so the test that proves the lock works cannot reach an unexported variable here.
// It exists only for that test: the window is genuinely small on a real machine, and
// a lock test that passes whether or not the lock is present is not a test.
func DelayReadsForTest(delay func()) func() {
	previous := readExisting
	readExisting = func(path string) ([]byte, error) {
		raw, err := os.ReadFile(path)
		delay()
		return raw, err
	}
	return func() { readExisting = previous }
}
