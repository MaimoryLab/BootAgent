//go:build darwin

package platform

import "syscall"

// nativeArch reports the architecture of the machine rather than of this
// process.
//
// runtime.GOARCH names the binary, and the two disagree under Rosetta 2: the
// amd64 build of BootAgent installed on an Apple Silicon Mac reports "amd64" on
// hardware that is arm64. Everything downstream then selects x64 packages --
// ZCode installs its x64 build, and DSH Desktop's macOS lookup refuses to
// resolve at all -- on a machine whose native packages exist and are faster.
//
// sysctl.proc_translated answers this directly and is readable from the
// translated process itself. uname/hw.machine is not usable here: it is
// translated too, and reports x86_64. Rosetta 2 only ever translates x86_64 on
// arm64 hardware, so a translated process is proof of an arm64 machine.
func nativeArch(goarch string) string {
	if goarch != "amd64" {
		return goarch
	}
	if translated, err := syscall.SysctlUint32("sysctl.proc_translated"); err == nil && translated == 1 {
		return "arm64"
	}
	return goarch
}
