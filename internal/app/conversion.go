package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/convertproxy"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

const converterPrefix = "bootagent_converter_"

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
		return ConversionConfig{Listen: "127.0.0.1:8787"}, nil
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
	if p, e := u.providers.Get(converterPrefix + "anthropic"); e == nil {
		c.APIKey = p.APIKey
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
	p, err := u.providers.Get(target.Provider)
	if err != nil {
		return err
	}
	return u.conversion.SetConfig(convertproxy.Config{Enabled: true, Listen: c.Listen, APIKey: c.APIKey, TargetBaseURL: p.BaseFor("openai"), TargetAPIKey: p.APIKey})
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
	p, err := u.providers.Get(target.Provider)
	if err != nil {
		return c, err
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return c, fmt.Errorf("target Provider has no API key")
	}
	if c.AnthropicModel == "" {
		c.AnthropicModel = stringValue(target.Model)
	}
	if c.ResponsesModel == "" {
		c.ResponsesModel = stringValue(target.Model)
	}
	for _, f := range []string{"anthropic", "responses"} {
		id := converterPrefix + f
		_, err = u.SaveProvider(ctx, provider.Entry{ID: id, Name: "BootAgent Converter " + f, BaseURL: "http://" + c.Listen, APIKey: c.APIKey}, false, false)
		if err != nil && !strings.Contains(err.Error(), "Unknown Provider") {
			return c, err
		}
		_, err = u.SaveProfile(ctx, SaveProfileOptions{ID: id, Label: "BootAgent Converter " + f, Provider: id, APIKey: c.APIKey, Model: map[string]string{"anthropic": c.AnthropicModel, "responses": c.ResponsesModel}[f], Protocol: map[string]string{"anthropic": "anthropic", "responses": "responses"}[f], ConfigMode: "provider"})
		if err != nil {
			return c, err
		}
	}
	b, _ := json.MarshalIndent(storedConversionConfig{Enabled: c.Enabled, Listen: c.Listen, TargetProfile: c.TargetProfile, AnthropicModel: c.AnthropicModel, ResponsesModel: c.ResponsesModel}, "", "  ")
	if err := os.MkdirAll(filepath.Dir(u.conversionPath()), 0700); err != nil {
		return c, err
	}
	if err := os.WriteFile(u.conversionPath(), append(b, '\n'), 0600); err != nil {
		return c, err
	}
	_ = u.conversion.SetConfig(convertproxy.Config{Enabled: c.Enabled, Listen: c.Listen, APIKey: c.APIKey, TargetBaseURL: p.BaseFor("openai"), TargetAPIKey: p.APIKey, Model: stringValue(target.Model)})
	return c, nil
}
func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
