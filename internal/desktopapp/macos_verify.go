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
)

func verifyMacOSApp(ctx context.Context, options Options, appPath string) error {
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
	for _, required := range []string{
		"Identifier=" + CodexBundleID,
		"TeamIdentifier=" + MacExpectedTeamID,
		"Authority=" + MacExpectedAuthority,
	} {
		if !strings.Contains(details, required) {
			return fmt.Errorf("macOS signature is missing expected identity %q", required)
		}
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
