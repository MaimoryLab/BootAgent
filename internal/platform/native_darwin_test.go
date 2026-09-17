//go:build darwin

package platform

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// nativeArch must not alter anything but a translated amd64 process: arm64 is
// already native, and on an Intel Mac amd64 is the truth.
func TestNativeArchLeavesUntranslatedValuesAlone(t *testing.T) {
	if got := nativeArch("arm64"); got != "arm64" {
		t.Errorf("nativeArch(arm64) = %q, want arm64", got)
	}
	if got := nativeArch("386"); got != "386" {
		t.Errorf("nativeArch(386) = %q, want 386", got)
	}
}

// The correction is checked against the machine rather than against a stub, so
// this asserts the real relationship: whatever sysctl reports, Current() must
// name the hardware. On an arm64 Mac the arm64 test binary is untranslated and
// the amd64 one is, and both have to arrive at arm64.
func TestCurrentReportsHardwareArchNotBinaryArch(t *testing.T) {
	out, err := exec.Command("/usr/sbin/sysctl", "-n", "hw.optional.arm64").Output()
	if err != nil {
		// Absent on Intel Macs, where GOARCH is already the hardware.
		if got := Current().Arch; got != "x64" {
			t.Errorf("Current().Arch on an Intel Mac = %q, want x64", got)
		}
		return
	}
	if strings.TrimSpace(string(out)) != "1" {
		t.Skip("hw.optional.arm64 is present but not 1")
	}
	if got := Current().Arch; got != "arm64" {
		t.Errorf("Current().Arch on arm64 hardware = %q (GOARCH=%s), want arm64", got, runtime.GOARCH)
	}
}
