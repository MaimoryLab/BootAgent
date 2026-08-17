package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/convertproxy"
	profileStore "github.com/MaimoryLab/BootAgent/internal/profile"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

const converterPrefix = "bootagent-converter-"
const legacyConverterPrefix = "bootagent_converter_"
const defaultAnthropicConversionModel = "claude-sonnet-5"
const defaultResponsesConversionModel = "gpt-5.6-sol"

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
	APIKey         string `json:"api_key"`
	TargetProfile  string `json:"target_profile"`
	AnthropicModel string `json:"anthropic_model"`
	ResponsesModel string `json:"responses_model"`
}

type storedConversionConfig struct {
	Enabled        bool   `json:"enabled"`
	Listen         string `json:"listen"`
	TargetProfile  string `json:"target_profile"`
	AnthropicModel string `json:"anthropic_model"`
	ResponsesModel string `json:"responses_model"`
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
		return ConversionConfig{Listen: "127.0.0.1:8787", AnthropicModel: defaultAnthropicConversionModel, ResponsesModel: defaultResponsesConversionModel}, nil
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
	for _, prefix := range []string{converterPrefix, legacyConverterPrefix} {
		if p, e := u.providers.Get(prefix + "anthropic"); e == nil {
			c.APIKey = p.APIKey
			break
		}
	}
	return c, nil
}

func (u *UseCases) startSavedConversion() error {
	c, err := u.Conversion(context.Background())
	if err != nil || !c.Enabled {
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
	return u.conversion.SetConfig(convertproxy.Config{Enabled: true, Listen: c.Listen, APIKey: c.APIKey, Models: []string{c.AnthropicModel, c.ResponsesModel}, TargetBaseURL: p.BaseFor("openai"), TargetModel: profileModel(target), TargetReasoningEffort: target.ReasoningEffort, TargetAPIKey: p.APIKey})
}
func (u *UseCases) SaveConversion(ctx context.Context, c ConversionConfig) (ConversionConfig, error) {
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
	if c.Enabled {
		for _, f := range []string{"anthropic", "responses"} {
			id := converterPrefix + f
			_, err = u.SaveProvider(ctx, provider.Entry{ID: id, Name: "BootAgent Converter " + f, BaseURL: provider.OpenAIBaseURL("http://" + c.Listen), APIKey: c.APIKey}, false, false)
			if err != nil && !errors.Is(err, provider.ErrUnknownProvider) {
				return c, err
			}
			_, err = u.SaveProfile(ctx, SaveProfileOptions{ID: id, Label: "BootAgent Converter " + f, Provider: id, APIKey: c.APIKey, Model: map[string]string{"anthropic": c.AnthropicModel, "responses": c.ResponsesModel}[f], Protocol: map[string]string{"anthropic": "anthropic", "responses": "responses"}[f], ConfigMode: "provider"})
			if err != nil {
				return c, err
			}
		}
	}
	b, _ := json.MarshalIndent(storedConversionConfig{Enabled: c.Enabled, Listen: c.Listen, TargetProfile: c.TargetProfile, AnthropicModel: c.AnthropicModel, ResponsesModel: c.ResponsesModel}, "", "  ")
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	if _, err := u.filesystem.AtomicWrite(ctx, u.conversionPath(), append(b, '\n'), false); err != nil {
		return c, err
	}
	if err := u.conversion.SetConfig(convertproxy.Config{Enabled: c.Enabled, Listen: c.Listen, APIKey: c.APIKey, Models: []string{c.AnthropicModel, c.ResponsesModel}, TargetBaseURL: p.BaseFor("openai"), TargetModel: profileModel(target), TargetReasoningEffort: target.ReasoningEffort, TargetAPIKey: p.APIKey}); err != nil {
		return c, err
	}
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
