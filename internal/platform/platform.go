// Package platform centralizes operating-system and home-directory decisions.
// It is deliberately injectable so catalog and status tests never touch the
// developer's real HOME.
package platform

import (
	"os"
	"runtime"
	"strings"
)

type Info struct {
	OS    string `json:"os"`
	Arch  string `json:"arch"`
	Shell string `json:"shell"`
}

func Current() Info {
	return For(runtime.GOOS, runtime.GOARCH)
}

func For(goos, goarch string) Info {
	osID := normalizeOS(goos)
	arch := "x64"
	if goarch == "arm64" || goarch == "aarch64" {
		arch = "arm64"
	}
	shell := "bash"
	if osID == "windows" {
		shell = "powershell"
	}
	return Info{OS: osID, Arch: arch, Shell: shell}
}

func normalizeOS(goos string) string {
	switch strings.ToLower(goos) {
	case "darwin", "macos":
		return "macos"
	case "windows", "win32":
		return "windows"
	default:
		// The product currently has a Linux target for every non-Darwin,
		// non-Windows Unix platform. This keeps the product's Linux fallback.
		return "linux"
	}
}

// ResolveHome uses native Windows variables, then HOME, then the process home.
func ResolveHome(env map[string]string, osID string) string {
	values := env
	if values == nil {
		values = environ()
	}
	if osID == "windows" {
		if value := values["USERPROFILE"]; value != "" {
			return value
		}
		if drive, path := values["HOMEDRIVE"], values["HOMEPATH"]; drive != "" && path != "" {
			return drive + path
		}
	}
	if value := values["HOME"]; value != "" {
		return value
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "."
}

func environ() map[string]string {
	values := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}
