package platform

import "strings"

// RegionCommand returns the locale lookup for this platform, or nil when the
// environment is the only source worth trusting.
//
// A lookup is needed at all because the environment is not enough: a macOS app
// launched from Finder inherits no LANG, and Windows does not use these
// variables. On Linux the environment is authoritative, so there is no command.
func RegionCommand(osID string) []string {
	switch osID {
	case "macos":
		// AppleLocale is the region ("zh_CN"), present even when the UI language
		// is English but the region is China.
		return []string{"defaults", "read", "-g", "AppleLocale"}
	case "windows":
		// The UI culture, the format culture and the home region all matter, so
		// read them together and let the parser decide.
		return []string{
			"powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command",
			"[System.Globalization.CultureInfo]::CurrentUICulture.Name; (Get-Culture).Name; (Get-WinHomeLocation).HomeLocation",
		}
	default:
		return nil
	}
}

// LocaleFromEnvironment follows the POSIX precedence: LC_ALL wins, then LANG,
// then LANGUAGE.
func LocaleFromEnvironment(env map[string]string) string {
	for _, name := range []string{"LC_ALL", "LANG", "LANGUAGE"} {
		if value := strings.TrimSpace(env[name]); value != "" {
			return value
		}
	}
	return ""
}

// IsChineseLocale recognizes the locale, language-tag and region spellings the
// three platforms report: "zh_CN.UTF-8" from a POSIX environment, "zh-Hans-CN"
// from macOS, "zh-CN" and a bare "CN" from PowerShell.
//
// It deliberately matches mainland China only. Hong Kong, Macau and Taiwan use
// zh as well, but the mirror hosts are mainland CDNs that are not necessarily
// faster from there, so defaulting them to it would be a guess that helps
// nobody. They can still turn the setting on.
func IsChineseLocale(value string) bool {
	for field := range strings.FieldsSeq(strings.TrimSpace(value)) {
		token := strings.ToLower(field)
		// Drop the codeset and modifier from "zh_CN.UTF-8" or "zh_CN@pinyin".
		if index := strings.IndexAny(token, ".@"); index >= 0 {
			token = token[:index]
		}
		token = strings.ReplaceAll(token, "_", "-")
		switch token {
		// Get-WinHomeLocation reports a region name rather than a locale.
		case "cn", "chn", "china":
			return true
		}
		// zh-CN, and zh-Hans-CN and friends: Simplified Chinese for the mainland.
		if token == "zh-cn" || (strings.HasPrefix(token, "zh-") && strings.HasSuffix(token, "-cn")) {
			return true
		}
	}
	return false
}
