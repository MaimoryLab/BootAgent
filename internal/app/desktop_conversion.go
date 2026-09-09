package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	configWriter "github.com/MaimoryLab/BootAgent/internal/config"
	"github.com/MaimoryLab/BootAgent/internal/convertproxy"
	"github.com/MaimoryLab/BootAgent/internal/desktopapp"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

const (
	DesktopProfileNative      = "native"
	DesktopProfileConvertible = "convertible"
	DesktopProfileUnusable    = "unusable"
	desktopCapabilityCacheTTL = 30 * time.Minute
)

type DesktopAgentProfileAssessment struct {
	AgentID          string `json:"agent"`
	ProfileID        string `json:"profileId"`
	Compatibility    string `json:"compatibility"`
	Message          string `json:"message"`
	ErrorCode        string `json:"errorCode,omitempty"`
	UpstreamVerified bool   `json:"upstreamVerified"`
}

type DesktopAgentConversionResult struct {
	DesktopAgentProfileResult
	Compatibility     string `json:"compatibility"`
	ConversionRunning bool   `json:"conversionRunning"`
	LocalAuthVerified bool   `json:"localAuthVerified"`
	UpstreamVerified  bool   `json:"upstreamVerified"`
	AgentConfigured   bool   `json:"agentConfigured"`
	EndToEndVerified  bool   `json:"endToEndVerified"`
}

func (u *UseCases) AssessDesktopAgentProfile(ctx context.Context, agentID, profileID string) (DesktopAgentProfileAssessment, error) {
	return u.assessDesktopAgentProfile(ctx, agentID, profileID, true)
}

func (u *UseCases) assessDesktopAgentProfile(ctx context.Context, agentID, profileID string, allowCache bool) (DesktopAgentProfileAssessment, error) {
	result := DesktopAgentProfileAssessment{AgentID: strings.TrimSpace(agentID), ProfileID: strings.TrimSpace(profileID), Compatibility: DesktopProfileUnusable}
	if result.AgentID != desktopapp.ClaudeDesktopID {
		return result, oneerrors.New(oneerrors.InvalidRequest, "Automatic protocol adaptation currently supports Claude Desktop")
	}
	profile := u.profileByID(result.ProfileID)
	if profile.ID == "" {
		return result, oneerrors.New(oneerrors.InvalidRequest, "Profile not found: "+result.ProfileID)
	}
	target, err := u.providers.Get(profile.Provider)
	if err != nil {
		return result, err
	}
	model := profileModel(profile)
	if model == "" || strings.TrimSpace(target.APIKey) == "" {
		result.Message = "Profile requires a model and Provider API key"
		return result, nil
	}
	if allowCache {
		if cached, ok := u.capabilities.Get(target, model); ok {
			age := time.Since(cached.ProbedAt)
			anthropic, hasAnthropic := cached.Protocols[provider.ProtocolAnthropic]
			chat, hasChat := cached.Protocols[provider.ProtocolOpenAI]
			if age >= 0 && age <= desktopCapabilityCacheTTL && hasAnthropic && hasChat {
				return desktopAssessmentFromCapabilities(result, profile.Protocol, anthropic, chat), nil
			}
		}
	}

	protocols := []string{provider.ProtocolAnthropic, provider.ProtocolOpenAI}
	verdicts, err := u.probeProtocols(ctx, protocols, target.APIKey, model, target.BaseFor)
	if err != nil {
		return result, err
	}
	capabilities := make(map[string]provider.ProtocolCapability, len(verdicts))
	for protocolID, verdict := range verdicts {
		capabilities[protocolID] = provider.CapabilityFromProbe(verdict)
	}
	// Capabilities are derived data. A cache write failure must not turn a
	// successful live compatibility check into a user-visible setup failure.
	_ = u.capabilities.Save(ctx, target, model, capabilities)
	anthropic := provider.CapabilityFromProbe(verdicts[provider.ProtocolAnthropic])
	chat := provider.CapabilityFromProbe(verdicts[provider.ProtocolOpenAI])
	result = desktopAssessmentFromCapabilities(result, profile.Protocol, anthropic, chat)
	if result.Compatibility == DesktopProfileUnusable {
		failed := verdicts[provider.ProtocolAnthropic]
		if profile.Protocol == provider.ProtocolOpenAI && !verdicts[provider.ProtocolOpenAI].OK {
			failed = verdicts[provider.ProtocolOpenAI]
		}
		result.Message = failed.Message
	}
	return result, nil
}

func desktopAssessmentFromCapabilities(result DesktopAgentProfileAssessment, profileProtocol string, anthropic, chat provider.ProtocolCapability) DesktopAgentProfileAssessment {
	if profileProtocol == provider.ProtocolAnthropic && anthropic.Supported {
		result.Compatibility, result.Message, result.UpstreamVerified = DesktopProfileNative, "Profile supports Anthropic Messages directly", true
		return result
	}
	if profileProtocol == provider.ProtocolOpenAI && !anthropic.Supported && chat.Supported {
		result.Compatibility, result.Message, result.UpstreamVerified = DesktopProfileConvertible, "Profile can be used through local protocol adaptation", true
		return result
	}
	failed := anthropic
	if profileProtocol == provider.ProtocolOpenAI && !chat.Supported {
		failed = chat
	}
	result.ErrorCode = failed.ErrorCode
	result.Message = "Profile could not be verified for Claude Desktop"
	return result
}

type managedFileSnapshot struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
	secret bool
}

func (u *UseCases) conversionStatePaths() ([]managedFileSnapshot, error) {
	paths := []managedFileSnapshot{
		{path: u.conversionPath()},
		{path: u.providers.Path(), secret: true},
	}
	for _, id := range []string{converterPrefix + "anthropic", converterPrefix + "responses", converterPrefix + "chat"} {
		profilePath, err := u.profiles.ProfilePath(id)
		if err != nil {
			return nil, err
		}
		secretPath, err := u.profiles.SecretPath(id)
		if err != nil {
			return nil, err
		}
		paths = append(paths, managedFileSnapshot{path: profilePath}, managedFileSnapshot{path: secretPath, secret: true})
	}
	return paths, nil
}

func (u *UseCases) conversionTransactionPaths() ([]managedFileSnapshot, error) {
	paths, err := u.conversionStatePaths()
	if err != nil {
		return nil, err
	}
	bindingPath, err := u.profiles.AgentBindingPath(desktopapp.ClaudeDesktopID)
	if err != nil {
		return nil, err
	}
	paths = append(paths, managedFileSnapshot{path: bindingPath})
	claudePaths, err := configWriter.ClaudeDesktopManagedPaths(u.status.Home, u.status.Platform.OS)
	if err != nil {
		return nil, err
	}
	for index, path := range claudePaths {
		paths = append(paths, managedFileSnapshot{path: path, secret: index == 0})
	}
	return paths, nil
}

func snapshotManagedFiles(paths []managedFileSnapshot) ([]managedFileSnapshot, error) {
	for index := range paths {
		data, err := os.ReadFile(paths[index].path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(paths[index].path)
		if err != nil {
			return nil, err
		}
		paths[index].data, paths[index].mode, paths[index].exists = data, info.Mode().Perm(), true
	}
	return paths, nil
}

func (u *UseCases) restoreManagedFiles(ctx context.Context, snapshots []managedFileSnapshot) error {
	var result error
	for index := len(snapshots) - 1; index >= 0; index-- {
		snapshot := snapshots[index]
		if snapshot.exists {
			if _, err := u.filesystem.AtomicWrite(ctx, snapshot.path, snapshot.data, snapshot.secret); err != nil {
				result = errors.Join(result, err)
				continue
			}
			if err := os.Chmod(snapshot.path, snapshot.mode); err != nil {
				result = errors.Join(result, err)
			}
		} else if err := os.Remove(snapshot.path); err != nil && !os.IsNotExist(err) {
			result = errors.Join(result, err)
		}
	}
	return result
}

func (u *UseCases) restoreConversionRuntime(previous ConversionConfig) error {
	if err := u.conversion.SetConfig(convertproxy.Config{Enabled: false}); err != nil {
		return err
	}
	if previous.Enabled {
		return u.startSavedConversion()
	}
	return nil
}

func (u *UseCases) rollbackConversionFailure(ctx context.Context, cause error, previous ConversionConfig, snapshots []managedFileSnapshot) error {
	rollbackErr := errors.Join(u.restoreManagedFiles(context.WithoutCancel(ctx), snapshots), u.restoreConversionRuntime(previous))
	if rollbackErr == nil {
		return cause
	}
	return oneerrors.New(oneerrors.ConversionRollbackFailed, "Protocol adaptation failed and its previous state could not be fully restored", oneerrors.WithStatus(500), oneerrors.WithCause(errors.Join(cause, rollbackErr)))
}

func verifyLocalConversion(ctx context.Context, listen, key, model string) error {
	base := "http://" + listen
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/models", nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("local conversion authentication check returned HTTP %d", response.StatusCode)
	}
	result, err := provider.NewClient(nil).Probe(ctx, provider.ProtocolAnthropic, "custom", key, model, base)
	if err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("local conversion verification failed: %s", result.Message)
	}
	return nil
}

func (u *UseCases) ConfigureDesktopAgentWithConversion(ctx context.Context, agentID, profileID string) (result DesktopAgentConversionResult, err error) {
	u.conversionMu.Lock()
	defer u.conversionMu.Unlock()
	// A write operation must not rely on a cached probe: the upstream has to
	// work at the moment Claude Desktop is changed.
	assessment, err := u.assessDesktopAgentProfile(ctx, agentID, profileID, false)
	if err != nil {
		return result, err
	}
	if assessment.Compatibility != DesktopProfileConvertible {
		code := assessment.ErrorCode
		if code == "" {
			code = oneerrors.ProtocolUnsupported
		}
		return result, oneerrors.New(code, assessment.Message)
	}
	previous, err := u.Conversion(ctx)
	if err != nil {
		return result, err
	}
	paths, err := u.conversionTransactionPaths()
	if err != nil {
		return result, err
	}
	snapshots, err := snapshotManagedFiles(paths)
	if err != nil {
		return result, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		err = u.rollbackConversionFailure(ctx, err, previous, snapshots)
	}()

	config := previous
	config.Enabled = true
	config.TargetProfile = profileID
	config, err = u.saveConversion(ctx, config)
	if err != nil {
		return result, err
	}
	key := u.conversionAPIKey()
	result.Compatibility = DesktopProfileConvertible
	result.UpstreamVerified = assessment.UpstreamVerified
	result.ConversionRunning = config.Enabled
	if err = verifyLocalConversion(ctx, config.Listen, key, config.AnthropicModel); err != nil {
		return result, oneerrors.New(oneerrors.ConversionVerificationFailed, "Local protocol adaptation verification failed", oneerrors.WithStatus(502), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	result.LocalAuthVerified, result.EndToEndVerified = true, true
	result.DesktopAgentProfileResult, err = u.ConfigureDesktopAgent(ctx, agentID, converterPrefix+"anthropic")
	if err != nil {
		return result, err
	}
	result.AgentConfigured = true
	committed = true
	return result, nil
}
