package desktopapp

import (
	"fmt"
	"net/url"
	"strings"
)

// approvedDownloadURL keeps the installer trust boundary on HTTPS endpoints
// owned by the vendor. Signature verification remains mandatory after download;
// an allowlisted host alone does not authenticate the bytes.
func approvedDownloadURL(raw string, allowedHosts ...string) (string, error) {
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("parse download URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return "", fmt.Errorf("download URL must be an HTTPS URL without credentials")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return "", fmt.Errorf("download URL must use HTTPS port 443")
	}
	host := strings.ToLower(parsed.Hostname())
	for _, allowed := range allowedHosts {
		if host == strings.ToLower(strings.TrimSpace(allowed)) {
			return parsed.String(), nil
		}
	}
	return "", fmt.Errorf("download URL host %q is not approved", host)
}
