//go:build !windows

package platform

// SystemRegion has a native implementation only on Windows. macOS uses
// AppleLocale and Linux uses its locale environment in region.go.
func SystemRegion() (string, bool) {
	return "", false
}
