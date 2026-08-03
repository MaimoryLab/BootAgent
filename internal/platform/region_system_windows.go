//go:build windows

package platform

import (
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	geoClassNation      = 16
	geoIDNotAvailable   = -1
	localeNameMaxLength = 85
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	getUserDefaultLocaleName = kernel32.NewProc("GetUserDefaultLocaleName")
	getUserGeoID             = kernel32.NewProc("GetUserGeoID")
)

// SystemRegion reads Windows' user locale and Home Location without spawning
// PowerShell. The latter is a numeric GeoID; mainland China is 45.
func SystemRegion() (string, bool) {
	values := make([]string, 0, 2)
	locale := make([]uint16, localeNameMaxLength)
	if count, _, _ := getUserDefaultLocaleName.Call(
		uintptr(unsafe.Pointer(&locale[0])),
		uintptr(len(locale)),
	); count > 0 {
		values = append(values, syscall.UTF16ToString(locale))
	}
	if geoID, _, _ := getUserGeoID.Call(geoClassNation); int32(geoID) != geoIDNotAvailable {
		values = append(values, strconv.FormatInt(int64(int32(geoID)), 10))
	}
	return strings.Join(values, "\n"), len(values) > 0
}
