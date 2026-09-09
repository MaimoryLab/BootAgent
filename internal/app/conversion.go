package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/convertproxy"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	profileStore "github.com/MaimoryLab/BootAgent/internal/profile"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

const converterPrefix = "bootagent-converter-"
const legacyConverterPrefix = "bootagent_converter_"
const defaultAnthropicConversionModel = "claude-sonnet-5"
const defaultResponsesConversionModel = "gpt-5.6-sol"
const defaultChatConversionModel = "gpt-5.6-sol"

func isConverterID(id string) bool {
	return strings.HasPrefix(id, converterPrefix) || strings.HasPrefix(id, legacyConverterPrefix)
}

func (u *UseCases) converterEnabled() bool {
	c, err := u.Conversion(context.Background())
	return err == nil && c.Enabled
}

func (u *UseCases) CloseConversion() error {
	if u == nil || u.conversion == nil {
		return nil
	}
	return u.conversion.Close()
}

type ConversionConfig struct {
	Enabled        bool   `json:"enabled"`
	Listen         string `json:"listen"`
	HasAPIKey      bool   `json:"has_api_key"`
	TargetProfile  string `json:"target_profile"`
	AnthropicModel string `json:"anthropic_model"`
	ResponsesModel string `json:"responses_model"`
	ChatModel      string `json:"chat_model"`
}

type storedConversionConfig struct {
	Enabled        bool   `json:"enabled"`
	Listen         string `json:"listen"`
	TargetProfile  string `json:"target_profile"`
	AnthropicModel string `json:"anthropic_model"`
	ResponsesModel string `json:"responses_model"`
	ChatModel      string `json:"chat_model"`
}

func (u *UseCases) conversionPath() string {
	return filepath.Join(u.status.Home, ".bootagent", "conversion.json")
}
func (u *UseCases) Conversion(ctx context.Context) (ConversionConfig, error) {
	if u == nil {
		return ConversionConfig{}, nil
	}
	b, err := os.ReadFile(u.conversionPath())
	if os.IsNotExist(err) {
		return ConversionConfig{Listen: "127.0.0.1:8787", AnthropicModel: defaultAnthropicConversionModel, ResponsesModel: defaultResponsesConversionModel, ChatModel: defaultChatConversionModel}, nil
	}
	if err != nil {
		return ConversionConfig{}, err
	}
	var c ConversionConfig
	if err = json.Unmarshal(b, &c); err != nil {
		return ConversionConfig{}, err
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8787"
	}
	if c.AnthropicModel == "" {
		c.AnthropicModel = defaultAnthropicConversionModel
	}
	if c.ResponsesModel == "" {
		c.ResponsesModel = defaultResponsesConversionModel
	}
	if c.ChatModel == "" {
		c.ChatModel = defaultChatConversionModel
	}
	c.HasAPIKey = u.conversionAPIKey() != ""
	return c, nil
}

func (u *UseCases) conversionAPIKey() string {
	for _, prefix := range []string{converterPrefix, legacyConverterPrefix} {
		if p, err := u.providers.Get(prefix + "anthropic"); err == nil && strings.TrimSpace(p.APIKey) != "" {
			return p.APIKey
		}
	}
	return ""
}

func newConversionAPIKey() (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("cannot generate local conversion API key: %w", err)
	}
	return "ba_" + base64.RawURLEncoding.EncodeToString(secret), nil
}

func validateConversionListen(value string) error {
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return oneerrors.New(oneerrors.InvalidRequest, "Conversion listen address must contain a host and numeric port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return oneerrors.New(oneerrors.InvalidRequest, "Conversion listen address must use a valid numeric port")
	}
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return oneerrors.New(oneerrors.InvalidRequest, "Conversion listen address must use a loopback host")
	}
	return nil
}

func (u *UseCases) startSavedConversion() error {
	c, err := u.Conversion(context.Background())
	if err != nil || !c.Enabled {
		return err
	}
	if err := validateConversionListen(c.Listen); err != nil {
		return err
	}
	target := u.profileByID(c.TargetProfile)
	if target.ID == "" {
		return nil
	}
	if target.Protocol != provider.ProtocolOpenAI {
		return fmt.Errorf("target Profile must use OpenAI Chat Completions")
	}
	p, err := u.providers.Get(target.Provider)
	if err != nil {
		return err
	}
	key := u.conversionAPIKey()
	if key == "" {
		return fmt.Errorf("local conversion API key is missing")
	}
	return u.conversion.SetConfig(convertproxy.Config{Enabled: true, Listen: c.Listen, APIKey: key, Models: []string{c.AnthropicModel, c.ResponsesModel, c.ChatModel}, TargetBaseURL: provider.OpenAIBaseURL(p.BaseFor("openai")), TargetModel: profileModel(target), TargetReasoningEffort: target.ReasoningEffort, TargetAPIKey: p.APIKey})
}
func (u *UseCases) SaveConversion(ctx context.Context, c ConversionConfig) (ConversionConfig, error) {
	if u == nil {
		return c, nil
	}
	u.conversionMu.Lock()
	defer u.conversionMu.Unlock()
	previous, err := u.Conversion(ctx)
	if err != nil {
		return c, err
	}
	paths, err := u.conversionStatePaths()
	if u.status.Platform.OS == "macos" || u.status.Platform.OS == "windows" {
		paths, err = u.conversionTransactionPaths()
	}
	if err != nil {
		return c, err
	}
	snapshots, err := snapshotManagedFiles(paths)
	if err != nil {
		return c, err
	}
	result, err := u.saveConversion(ctx, c)
	if err != nil {
		return result, u.rollbackConversionFailure(ctx, err, previous, snapshots)
	}
	return result, nil
}

func (u *UseCases) saveConversion(ctx context.Context, c ConversionConfig) (ConversionConfig, error) {
	if u == nil {
		return c, nil
	}
	if err := ctx.Err(); err != nil {
		return c, err
	}
	c.Listen = strings.TrimSpace(c.Listen)
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8787"
	}
	if err := validateConversionListen(c.Listen); err != nil {
		return c, err
	}
	if c.TargetProfile == "" {
		return c, fmt.Errorf("target profile is required")
	}
	target := u.profileByID(c.TargetProfile)
	if target.ID == "" {
		return c, fmt.Errorf("target Profile not found: %s", c.TargetProfile)
	}
	if target.Protocol != provider.ProtocolOpenAI {
		return c, fmt.Errorf("target Profile must use OpenAI Chat Completions")
	}
	p, err := u.providers.Get(target.Provider)
	if err != nil {
		return c, err
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return c, fmt.Errorf("target Provider has no API key")
	}
	if c.AnthropicModel == "" {
		c.AnthropicModel = defaultAnthropicConversionModel
	}
	if c.ResponsesModel == "" {
		c.ResponsesModel = defaultResponsesConversionModel
	}
	if c.ChatModel == "" {
		c.ChatModel = defaultChatConversionModel
	}
	key := u.conversionAPIKey()
	if c.Enabled {
		if key == "" {
			key, err = newConversionAPIKey()
			if err != nil {
				return c, err
			}
		}
		origin := provider.AnthropicClientBaseURL("http://" + c.Listen)
		openAIBase := provider.OpenAIBaseURL(origin)
		for _, f := range []string{"anthropic", "responses", "chat"} {
			id := converterPrefix + f
			entry := provider.Entry{ID: id, Name: "BootAgent Converter " + f, BaseURL: openAIBase, APIKey: key}
			if f == "anthropic" {
				entry.AnthropicBaseURL = origin
			}
			_, err = u.SaveProvider(ctx, entry, false, false)
			if err != nil && !errors.Is(err, provider.ErrUnknownProvider) {
				return c, err
			}
			_, err = u.SaveProfile(ctx, SaveProfileOptions{ID: id, Label: "BootAgent Converter " + f, Provider: id, Model: map[string]string{"anthropic": c.AnthropicModel, "responses": c.ResponsesModel, "chat": c.ChatModel}[f], Protocol: map[string]string{"anthropic": "anthropic", "responses": "responses", "chat": "openai"}[f], ConfigMode: "provider"})
			if err != nil {
				return c, err
			}
		}
	}
	b, _ := json.MarshalIndent(storedConversionConfig{Enabled: c.Enabled, Listen: c.Listen, TargetProfile: c.TargetProfile, AnthropicModel: c.AnthropicModel, ResponsesModel: c.ResponsesModel, ChatModel: c.ChatModel}, "", "  ")
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	if _, err := u.filesystem.AtomicWrite(ctx, u.conversionPath(), append(b, '\n'), false); err != nil {
		return c, err
	}
	if err := u.conversion.SetConfig(convertproxy.Config{Enabled: c.Enabled, Listen: c.Listen, APIKey: key, Models: []string{c.AnthropicModel, c.ResponsesModel, c.ChatModel}, TargetBaseURL: provider.OpenAIBaseURL(p.BaseFor("openai")), TargetModel: profileModel(target), TargetReasoningEffort: target.ReasoningEffort, TargetAPIKey: p.APIKey}); err != nil {
		return c, oneerrors.New(oneerrors.ConversionPortUnavailable, "The local protocol adapter could not listen on the configured port", oneerrors.WithStatus(409), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	c.HasAPIKey = key != ""
	return c, nil
}

func profileModel(profile profileStore.Profile) string {
	if profile.Model == nil {
		return ""
	}
	return strings.TrimSpace(*profile.Model)
}

func (u *UseCases) SetConversionEnabled(ctx context.Context, enabled bool) (ConversionConfig, error) {
	c, err := u.Conversion(ctx)
	if err != nil {
		return c, err
	}
	c.Enabled = enabled
	return u.SaveConversion(ctx, c)
}

func (u *UseCases) SetConversionTargetProfile(ctx context.Context, profileID string) (ConversionConfig, error) {
	c, err := u.Conversion(ctx)
	if err != nil {
		return c, err
	}
	c.TargetProfile = strings.TrimSpace(profileID)
	return u.SaveConversion(ctx, c)
}

func (u *UseCases) RegenerateConversionAPIKey(ctx context.Context) (result ConversionConfig, err error) {
	if u == nil {
		return result, oneerrors.New(oneerrors.InternalError, "Conversion service is not configured")
	}
	u.conversionMu.Lock()
	defer u.conversionMu.Unlock()
	previous, err := u.Conversion(ctx)
	if err != nil {
		return result, err
	}
	paths, err := u.conversionStatePaths()
	if u.status.Platform.OS == "macos" || u.status.Platform.OS == "windows" {
		paths, err = u.conversionTransactionPaths()
	}
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
	key, err := newConversionAPIKey()
	if err != nil {
		return result, err
	}
	for _, kind := range []string{"anthropic", "responses", "chat"} {
		entry, getErr := u.providers.Get(converterPrefix + kind)
		if getErr != nil {
			return result, getErr
		}
		entry.APIKey = key
		if _, saveErr := u.SaveProvider(ctx, entry, false, false); saveErr != nil {
			return result, saveErr
		}
	}
	result, err = u.saveConversion(ctx, previous)
	if err != nil {
		return result, err
	}
	committed = true
	return result, nil
}
