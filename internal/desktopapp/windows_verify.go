package desktopapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	windowsExpectedSignerOrganization = "Microsoft Corporation"
	windowsExpectedSignerPublisher    = "Microsoft Corporation"
)

type windowsAuthenticodeSignature struct {
	Status        string `json:"Status"`
	StatusMessage string `json:"StatusMessage"`
	Publisher     string `json:"Publisher"`
	Organization  string `json:"Organization"`
	Subject       string `json:"Subject"`
	Issuer        string `json:"Issuer"`
}

// verifyWindowsInstaller delegates trust evaluation to Windows' Authenticode
// verifier. A valid signature from an unexpected publisher is not sufficient:
// the downloaded bootstrapper must be the Microsoft-published installer used by
// Windows' official get.microsoft.com flow.
func verifyChatGPTWindowsInstaller(ctx context.Context, options Options, installerPath string) error {
	return verifyWindowsInstallerPublisher(ctx, options, installerPath, []string{windowsExpectedSignerOrganization})
}

func verifyWorkBuddyWindowsInstaller(ctx context.Context, edition workBuddyEdition, options Options, installerPath string) error {
	return verifyWindowsInstallerPublisher(ctx, options, installerPath, edition.windowsSigners)
}

// verifyZCodeWindowsInstaller pins the EV code-signing subject read out of the
// vendor's own published .exe -- O and CN both carry this value -- rather than a
// name taken from documentation. It is the same legal entity behind the macOS
// Developer ID team ZCodeMacTeamID.
func verifyZCodeWindowsInstaller(ctx context.Context, options Options, installerPath string) error {
	return verifyWindowsInstallerPublisher(ctx, options, installerPath, []string{"北京智谱华章科技股份有限公司"})
}

func verifyWindowsInstallerPublisher(ctx context.Context, options Options, installerPath string, allowed []string) error {
	if strings.TrimSpace(installerPath) == "" {
		return errors.New("Windows installer path is empty")
	}

	result, err := runWithEnvironment(
		options,
		ctx,
		append(windowsAuthenticodeQuery(), installerPath),
		nil,
		installTimeout,
	)
	if err != nil {
		return fmt.Errorf("run Windows Authenticode verification: %w", err)
	}
	if result.ExitCode != 0 {
		return commandFailure("run Windows Authenticode verification", result)
	}

	signature, err := parseWindowsAuthenticodeSignature(result.Stdout)
	if err != nil {
		return fmt.Errorf("parse Windows Authenticode result: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(signature.Status), "Valid") {
		status := strings.TrimSpace(signature.Status)
		if status == "" {
			status = "unknown"
		}
		message := strings.TrimSpace(signature.StatusMessage)
		if message == "" {
			return fmt.Errorf("Windows Authenticode status is %q", status)
		}
		return fmt.Errorf("Windows Authenticode status is %q: %s", status, message)
	}
	if strings.TrimSpace(signature.Subject) == "" || strings.TrimSpace(signature.Issuer) == "" {
		return errors.New("Windows Authenticode result has no signer certificate")
	}
	if !approvedWindowsSigner(signature.Organization, allowed) || !approvedWindowsSigner(signature.Publisher, allowed) {
		return fmt.Errorf("Windows Authenticode publisher %q (organization %q) is not approved", signature.Publisher, signature.Organization)
	}

	return nil
}

func approvedWindowsSigner(value string, allowed []string) bool {
	value = strings.TrimSpace(value)
	for _, expected := range allowed {
		if strings.EqualFold(value, expected) {
			return true
		}
	}
	return false
}

func windowsAuthenticodeQuery() []string {
	const script = `$signature = Get-AuthenticodeSignature -LiteralPath $args[0]
$certificate = $signature.SignerCertificate
$organization = ""
$publisher = ""
$subject = ""
$issuer = ""
if ($null -ne $certificate) {
  $publisher = [string]$certificate.GetNameInfo([System.Security.Cryptography.X509Certificates.X509NameType]::SimpleName, $false)
  $subject = [string]$certificate.Subject
  $issuer = [string]$certificate.Issuer
  $organization = ($subject -split "," | Where-Object { $_.TrimStart().StartsWith("O=") } | Select-Object -First 1)
  if ($null -ne $organization) { $organization = $organization.Substring($organization.IndexOf("=") + 1).Trim() }
}
[pscustomobject]@{
  Status = [string]$signature.Status
  StatusMessage = [string]$signature.StatusMessage
  Publisher = $publisher
  Organization = $organization
  Subject = $subject
  Issuer = $issuer
} | ConvertTo-Json -Compress`
	return []string{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script}
}

func parseWindowsAuthenticodeSignature(output string) (windowsAuthenticodeSignature, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(output), "\ufeff")
	if trimmed == "" {
		return windowsAuthenticodeSignature{}, errors.New("Windows Authenticode returned no result")
	}
	var signature windowsAuthenticodeSignature
	if err := json.Unmarshal([]byte(trimmed), &signature); err != nil {
		return windowsAuthenticodeSignature{}, err
	}
	return signature, nil
}
