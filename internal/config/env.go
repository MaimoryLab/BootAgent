package config

import (
	"context"
	"strings"

	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

func WriteAgentEnv(ctx context.Context, filesystem securefs.Store, path, osID, agentID, apiKey, baseURL, model, smallFastModel string, native map[string]string) error {
	values := []envValue{
		{Name: agentEnvVar(agentID), Value: apiKey},
		{Name: agentEnvVar(agentID, "API_BASE_URL"), Value: baseURL},
		{Name: "ONEAGENT_API_KEY", Value: apiKey},
		{Name: "ONEAGENT_API_BASE_URL", Value: baseURL},
	}
	fastModel := smallFastModel
	if fastModel == "" {
		fastModel = model
	}
	for _, item := range []struct{ field, value string }{
		{field: "api_key", value: apiKey},
		{field: "base_url", value: baseURL},
		{field: "model", value: model},
		{field: "small_fast_model", value: fastModel},
	} {
		field, value := item.field, item.value
		if name := native[field]; name != "" && value != "" {
			values = append(values, envValue{Name: name, Value: value})
		}
	}
	content := renderEnv(osID, values)
	if _, err := filesystem.AtomicWrite(ctx, path, []byte(content), true); err != nil {
		return err
	}
	return nil
}

func WriteSharedEnv(ctx context.Context, filesystem securefs.Store, path, osID, apiKey, baseURL string) error {
	content := renderEnv(osID, []envValue{
		{Name: "ONEAGENT_API_KEY", Value: apiKey},
		{Name: "ONEAGENT_API_BASE_URL", Value: baseURL},
	})
	if _, err := filesystem.AtomicWrite(ctx, path, []byte(content), true); err != nil {
		return err
	}
	return nil
}

type envValue struct {
	Name  string
	Value string
}

func renderEnv(osID string, values []envValue) string {
	var builder strings.Builder
	for _, value := range values {
		if osID == "windows" {
			builder.WriteString("$env:")
			builder.WriteString(value.Name)
			builder.WriteString(" = '")
			builder.WriteString(powershellQuote(value.Value))
			builder.WriteString("'\n")
			continue
		}
		builder.WriteString("export ")
		builder.WriteString(value.Name)
		builder.WriteByte('=')
		builder.WriteString(shellQuote(value.Value))
		builder.WriteByte('\n')
	}
	return builder.String()
}

func agentEnvVar(agentID string, suffix ...string) string {
	stem := strings.Trim(strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' {
			return character - ('a' - 'A')
		}
		if character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			return character
		}
		return '_'
	}, agentID), "_")
	if len(suffix) == 0 {
		return "ONEAGENT_API_KEY_" + stem
	}
	if suffix[0] == "API_BASE_URL" {
		return "ONEAGENT_API_BASE_URL_" + stem
	}
	return "ONEAGENT_" + suffix[0] + "_" + stem
}
