package desktopapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	MacExpectedTeamID      = "2DC432GLL2"
	MacExpectedAuthority   = "Developer ID Application: OpenAI OpCo, LLC (2DC432GLL2)"
	MacExpectedSpctlSource = "source=Notarized Developer ID"
	WorkBuddyMacTeamID     = "FN2V63AD2J"
)

func verifyChatGPTMacOSApp(ctx context.Context, options Options, appPath string) error {
	return verifyMacOSIdentity(ctx, options, appPath, CodexBundleID, MacExpectedTeamID, MacExpectedAuthority)
}

func verifyWorkBuddyMacOSApp(ctx context.Context, edition workBuddyEdition, options Options, appPath string) error {
	return verifyMacOSIdentity(ctx, options, appPath, edition.bundleID, edition.macTeamID, "")
}

// verifyZCodeMacOSApp checks the downloaded bundle against ZCode's Developer ID
// team. No Authority literal is pinned: the empty argument makes
// verifyMacOSIdentity require a Developer ID authority ending in this team, which
// keeps the check from breaking when the vendor's certificate common name
// changes. Notarization is still required, via spctl.
func verifyZCodeMacOSApp(ctx context.Context, options Options, appPath string) error {
	return verifyMacOSIdentity(ctx, options, appPath, ZCodeBundleID, ZCodeMacTeamID, "")
}

func verifyMacOSIdentity(ctx context.Context, options Options, appPath, bundleID, teamID, authority string) error {
	result, err := run(options, ctx, []string{"/usr/bin/codesign", "--verify", "--deep", "--strict", "--verbose=2", appPath}, installTimeout)
	if err != nil {
		return fmt.Errorf("verify macOS code signature: %w", err)
	}
	if result.ExitCode != 0 {
		return commandFailure("verify macOS code signature", result)
	}

	result, err = run(options, ctx, []string{"/usr/bin/codesign", "-dv", "--verbose=4", appPath}, installTimeout)
	if err != nil {
		return fmt.Errorf("read macOS signing identity: %w", err)
	}
	if result.ExitCode != 0 {
		return commandFailure("read macOS signing identity", result)
	}
	details := macOSVerificationValues(result.Stdout + "\n" + result.Stderr)
	requiredValues := []struct{ key, value string }{
		{"Identifier", bundleID},
		{"TeamIdentifier", teamID},
	}
	for _, required := range requiredValues {
		values := details[required.key]
		if len(values) != 1 || values[0] != required.value {
			return fmt.Errorf("macOS signature is missing expected identity %q", required.key+"="+required.value)
		}
	}
	authorities := details["Authority"]
	if authority != "" && (len(authorities) == 0 || authorities[0] != authority) {
		return fmt.Errorf("macOS signature is missing expected identity %q", "Authority="+authority)
	}
	if authority == "" && (len(authorities) == 0 || !strings.HasPrefix(authorities[0], "Developer ID Application: ") || !strings.HasSuffix(authorities[0], " ("+teamID+")")) {
		return fmt.Errorf("macOS signature is missing the expected Developer ID authority for team %s", teamID)
	}

	result, err = run(options, ctx, []string{"/usr/sbin/spctl", "--assess", "--type", "execute", "--verbose=4", appPath}, installTimeout)
	if err != nil {
		return fmt.Errorf("assess macOS app with Gatekeeper: %w", err)
	}
	if result.ExitCode != 0 {
		return commandFailure("assess macOS app with Gatekeeper", result)
	}
	sources := macOSVerificationValues(result.Stdout + "\n" + result.Stderr)["source"]
	if len(sources) != 1 || "source="+sources[0] != MacExpectedSpctlSource {
		return errors.New("macOS app is not accepted as notarized Developer ID software")
	}

	return nil
}

func macOSVerificationValues(output string) map[string][]string {
	values := make(map[string][]string)
	for line := range strings.SplitSeq(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
		if !ok {
			continue
		}
		switch key {
		case "Identifier", "source":
			values[key] = append(values[key], value)
		case "TeamIdentifier", "Authority":
			// codesign emits signing fields after Identifier; anything earlier can be part of a newline-containing path.
			if len(values["Identifier"]) > 0 {
				values[key] = append(values[key], value)
			}
		}
	}
	return values
}
