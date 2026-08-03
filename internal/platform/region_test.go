package platform

import (
	"strings"
	"testing"
)

func TestIsChineseLocaleAcceptsWhatEachPlatformReports(t *testing.T) {
	for _, value := range []string{
		"zh_CN.UTF-8",   // POSIX LANG
		"en_CN.UTF-8",   // English UI, China region
		"zh_CN",         // macOS AppleLocale
		"en-CN",         // BCP-47 locale with an English UI
		"zh-Hans-CN",    // macOS AppleLanguages
		"zh-CN",         // .NET culture name
		"CN",            // Get-WinHomeLocation
		"45",            // Windows GeoId for mainland China
		"zh_CN@pinyin",  // locale with a modifier
		"zh-Hant-CN",    // unusual, but still the mainland region
		"en-US\nzh-CN",  // PowerShell prints several lines
		"  zh_CN.utf8 ", // surrounding whitespace and lowercase codeset
	} {
		if !IsChineseLocale(value) {
			t.Errorf("IsChineseLocale(%q) = false, want true", value)
		}
	}
}

// Matching too broadly is worse than matching too narrowly: a machine sent to a
// mainland CDN it cannot reach quickly is slower than the official source, and
// the user never asked for it.
func TestIsChineseLocaleRejectsEverythingElse(t *testing.T) {
	for _, value := range []string{
		"",
		"en_US.UTF-8",
		"ja_JP.UTF-8",
		"zh_TW.UTF-8", // Taiwan
		"zh-Hant-TW",
		"zh_HK.UTF-8", // Hong Kong
		"zh-MO",       // Macau
		"C",
		"POSIX",
		"cn_something_else",
		"US",
	} {
		if IsChineseLocale(value) {
			t.Errorf("IsChineseLocale(%q) = true, want false", value)
		}
	}
}

func TestLocaleFromEnvironmentFollowsPOSIXPrecedence(t *testing.T) {
	if got := LocaleFromEnvironment(map[string]string{"LANG": "en_US.UTF-8", "LC_ALL": "zh_CN.UTF-8"}); got != "zh_CN.UTF-8" {
		t.Fatalf("LC_ALL should win, got %q", got)
	}
	if got := LocaleFromEnvironment(map[string]string{"LANG": "zh_CN.UTF-8", "LANGUAGE": "en"}); got != "zh_CN.UTF-8" {
		t.Fatalf("LANG should beat LANGUAGE, got %q", got)
	}
	if got := LocaleFromEnvironment(map[string]string{"LANG": "  ", "LANGUAGE": "zh_CN"}); got != "zh_CN" {
		t.Fatalf("a blank LANG should not mask LANGUAGE, got %q", got)
	}
	if got := LocaleFromEnvironment(nil); got != "" {
		t.Fatalf("LocaleFromEnvironment(nil) = %q", got)
	}
}

// Linux reads the environment only. macOS and Windows need a lookup because a
// GUI app inherits no LANG there.
func TestRegionCommandExistsOnlyWhereTheEnvironmentIsInsufficient(t *testing.T) {
	if command := RegionCommand("linux"); command != nil {
		t.Fatalf("linux should need no lookup, got %v", command)
	}
	if command := RegionCommand("macos"); len(command) == 0 || command[0] != "defaults" {
		t.Fatalf("macos lookup = %v", command)
	}
	if command := RegionCommand("windows"); len(command) == 0 || command[0] != "powershell" {
		t.Fatalf("windows lookup = %v", command)
	}
	if command := RegionCommand("windows"); !strings.Contains(command[len(command)-1], ".GeoId") {
		t.Fatalf("windows lookup must read GeoId, got %v", command)
	}
}
