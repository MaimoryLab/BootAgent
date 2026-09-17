//go:build !darwin

package platform

// nativeArch is the identity outside macOS. Windows on ARM also runs x64
// processes under emulation, but its x64 packages are the supported way to
// install there, so correcting the value would select packages the vendors do
// not publish.
func nativeArch(goarch string) string {
	return goarch
}
