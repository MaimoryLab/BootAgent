package testutil

import "fmt"

// FakeReporter records what a double reported instead of failing a real test.
//
// It exists because these doubles have a guarantee worth testing: an
// unanticipated command must fail the test rather than answer with success.
// Proving that needs a Reporter whose failure can be inspected afterwards, and
// *testing.T is not one -- Fatalf unwinds the goroutine.
type FakeReporter struct {
	Errors  []string
	Fatals  []string
	Helpers int
	// stop is how Fatal and Fatalf mimic the control flow of the real thing.
	// Without it a double would keep running past the point where a real test
	// would have stopped, and the assertion after it would be meaningless.
	stop func()
}

// NewFakeReporter builds a reporter whose Fatal calls run stop, if given.
func NewFakeReporter(stop func()) *FakeReporter {
	return &FakeReporter{stop: stop}
}

// Errorf records a non-fatal failure.
func (f *FakeReporter) Errorf(format string, args ...any) {
	f.Errors = append(f.Errors, fmt.Sprintf(format, args...))
}

// Fatal records a fatal failure and stops, if a stop function was given.
func (f *FakeReporter) Fatal(args ...any) {
	f.Fatals = append(f.Fatals, fmt.Sprint(args...))
	f.halt()
}

// Fatalf records a formatted fatal failure and stops.
func (f *FakeReporter) Fatalf(format string, args ...any) {
	f.Fatals = append(f.Fatals, fmt.Sprintf(format, args...))
	f.halt()
}

// Helper counts the call, so a test can assert the double marked itself.
func (f *FakeReporter) Helper() { f.Helpers++ }

// Failed reports whether anything was recorded.
func (f *FakeReporter) Failed() bool { return len(f.Errors) > 0 || len(f.Fatals) > 0 }

// Messages returns every recorded failure, fatal ones last.
func (f *FakeReporter) Messages() []string {
	return append(append([]string(nil), f.Errors...), f.Fatals...)
}

func (f *FakeReporter) halt() {
	if f.stop != nil {
		f.stop()
	}
}
