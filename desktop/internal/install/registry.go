package install

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

// ResolveRegistry resolves a mirror id or explicit URL to a registry address.
//
// HTTPS only, and credentials are refused: a registry URL ends up in the
// installer environment and in the install log, so a token embedded in it would
// leak into both. ValidateBaseURL is deliberately not reused -- it permits
// http://, which is acceptable for a Provider endpoint the user names but not
// for the address a package is fetched from.
func ResolveRegistry(value string) (string, error) {
	if value == "" {
		return catalog.OfficialNpmRegistry, nil
	}
	if mirror, known := catalog.MirrorByID(value); known {
		return mirror.Registry, nil
	}
	for _, char := range value {
		if char < 32 || char == 127 {
			return "", oerr.New("INVALID_REQUEST", "Registry URL contains control characters")
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", oerr.New("INVALID_REQUEST", "Registry URL must start with https://")
	}
	if parsed.User != nil {
		return "", oerr.New("INVALID_REQUEST", "Registry URL must not contain credentials")
	}
	if !strings.HasSuffix(value, "/") {
		return value + "/", nil
	}
	return value, nil
}

// VerifyNpmIntegrity checks the registry's integrity for a pinned spec against
// the manifest.
//
// The manifest records a sha512 for every npm package but nothing ever read it,
// so the version was pinned and the bytes were not. That gap only becomes
// exploitable once a mirror is allowed: npm verifies a download against the
// integrity the registry itself served, which secures the transfer but takes the
// registry's word for what the package is. Comparing that value with the manifest
// closes the loop -- npm proves the bytes match what the registry declared, and
// this proves the declaration matches the official release.
func VerifyNpmIntegrity(rt *runtime.Runtime, npm, spec, expected, registry string, timeout time.Duration) error {
	if expected == "" {
		return nil
	}
	argv := []string{npm, "view", spec, "dist.integrity", "--registry=" + registry}
	result, err := rt.Run(context.Background(), argv, runtime.RunOptions{Env: rt.Env, Timeout: timeout})
	if err != nil {
		if runtime.IsTimeout(err) {
			// AGENT_INSTALL_FAILED rather than TIMEOUT, matching the Python call
			// site: the two timeouts in this package map to different codes
			// deliberately, which is why the runtime does not choose for them.
			return oerr.Newf("AGENT_INSTALL_FAILED", "Timed out reading the checksum for %s", spec).
				Set(oerr.WithRetryable())
		}
		return oerr.Newf("AGENT_INSTALL_FAILED", "Cannot read the checksum for %s: %v", spec, err)
	}
	if result.ExitCode != 0 {
		return oerr.Newf("AGENT_INSTALL_FAILED", "%s is not available on %s", spec, registry).
			Set(oerr.WithRetryable())
	}
	reported := strings.TrimSpace(result.Stdout)
	if reported != expected {
		// Fail closed and name both values: a mismatch means the registry is
		// serving something other than the locked release, which is exactly the
		// case a mirror has to be held to.
		shown := reported
		if shown == "" {
			shown = "(none)"
		}
		return oerr.Newf(
			"AGENT_INSTALL_FAILED",
			"Checksum mismatch for %s on %s: manifest expects %s, registry reports %s",
			spec, registry, expected, shown,
		)
	}
	return nil
}
