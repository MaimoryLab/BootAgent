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

func verifyMacOSApp(ctx context.Context, options Options, appPath string) error {
	return verifyMacOSIdentity(ctx, options, appPath, CodexBundleID, MacExpectedTeamID, MacExpectedAuthority)
}

func verifyWorkBuddyMacOSApp(ctx context.Context, options Options, appPath string) error {
	return verifyMacOSIdentity(ctx, options, appPath, WorkBuddyBundleID, WorkBuddyMacTeamID, "")
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
	details := result.Stdout + "\n" + result.Stderr
	requiredValues := []string{"Identifier=" + bundleID, "TeamIdentifier=" + teamID}
	if authority != "" {
		requiredValues = append(requiredValues, "Authority="+authority)
	}
	for _, required := range requiredValues {
		if !strings.Contains(details, required) {
			return fmt.Errorf("macOS signature is missing expected identity %q", required)
		}
	}
	if authority == "" && (!strings.Contains(details, "Authority=Developer ID Application:") || !strings.Contains(details, "("+teamID+")")) {
		return fmt.Errorf("macOS signature is missing the expected Developer ID authority for team %s", teamID)
	}

	result, err = run(options, ctx, []string{"/usr/sbin/spctl", "--assess", "--type", "execute", "--verbose=4", appPath}, installTimeout)
	if err != nil {
		return fmt.Errorf("assess macOS app with Gatekeeper: %w", err)
	}
	if result.ExitCode != 0 {
		return commandFailure("assess macOS app with Gatekeeper", result)
	}
	if !strings.Contains(result.Stdout+"\n"+result.Stderr, MacExpectedSpctlSource) {
		return errors.New("macOS app is not accepted as notarized Developer ID software")
	}

	return nil
}
