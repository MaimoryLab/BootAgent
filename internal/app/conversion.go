package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	"github.com/MaimoryLab/BootAgent/internal/convertproxy"
	"github.com/MaimoryLab/BootAgent/internal/desktopapp"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	profileStore "github.com/MaimoryLab/BootAgent/internal/profile"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

const converterPrefix = "bootagent-converter-"
const legacyConverterPrefix = "bootagent_converter_"

// converterRoutePrefix names an Agent's own converter Provider and Profile.
// Compatibility mode gives each Agent a route of its own -- a local key that
// names the model service its requests go to -- so two Agents can use two
// different model services through the one adapter at the same time.
//
// The shared converterPrefix+"anthropic"/"responses"/"chat" entries predate
// routes. Agents still pointed at them keep working: their key resolves to
// TargetProfile, the single target compatibility mode used to have.
const converterRoutePrefix = converterPrefix + "for-"

const defaultAnthropicConversionModel = "claude-sonnet-5"
const defaultResponsesConversionModel = "gpt-5.6-sol"
const defaultChatConversionModel = "gpt-5.6-sol"

func isConverterID(id string) bool {
	return strings.HasPrefix(id, converterPrefix) || strings.HasPrefix(id, legacyConverterPrefix)
}

// converterRouteID is the Provider and Profile ID of an Agent's own route.
func converterRouteID(agentID string) string { return converterRoutePrefix + agentID }

// routeAgent is the Agent a route Provider or Profile belongs to, if it is one.
func routeAgent(id string) (string, bool) {
	agentID, ok := strings.CutPrefix(id, converterRoutePrefix)
	return agentID, ok && agentID != ""
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
	Enabled   bool   `json:"enabled"`
	Listen    string `json:"listen"`
	HasAPIKey bool   `json:"has_api_key"`
	// TargetProfile is where Agents still on the shared converter entries are
	// forwarded. New Agents get a route of their own instead.
	TargetProfile string `json:"target_profile"`
	// Routes maps an Agent to the Chat Completions Profile its requests are
	// forwarded to. Written by ConfigureAgentWithConversion; SaveConversion
	// keeps what is stored.
	Routes         map[string]string `json:"routes"`
	AnthropicModel string            `json:"anthropic_model"`
	ResponsesModel string            `json:"responses_model"`
	ChatModel      string            `json:"chat_model"`
}

type storedConversionConfig struct {
	Enabled        bool              `json:"enabled"`
	Listen         string            `json:"listen"`
	TargetProfile  string            `json:"target_profile"`
	Routes         map[string]string `json:"routes,omitempty"`
	AnthropicModel string            `json:"anthropic_model"`
	ResponsesModel string            `json:"responses_model"`
	ChatModel      string            `json:"chat_model"`
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
		return ConversionConfig{Listen: "127.0.0.1:8787", Routes: map[string]string{}, AnthropicModel: defaultAnthropicConversionModel, ResponsesModel: defaultResponsesConversionModel, ChatModel: defaultChatConversionModel}, nil
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
	if c.Routes == nil {
		c.Routes = map[string]string{}
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
	for agentID := range c.Routes {
		if p, err := u.providers.Get(converterRouteID(agentID)); err == nil && p.APIKey != "" {
			c.HasAPIKey = true
		}
	}
	return c, nil
}

// conversionAPIKey is the key shared by the pre-route converter entries.
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

// conversionTarget is where a request for `profileID` is forwarded, or false
// when that Profile is gone, no longer speaks Chat Completions, or its
// Provider has lost its key.
func (u *UseCases) conversionTarget(profileID string) (convertproxy.Target, bool) {
	target := u.profileByID(profileID)
	if target.ID == "" || target.Protocol != provider.ProtocolOpenAI {
		return convertproxy.Target{}, false
	}
	p, err := u.providers.Get(target.Provider)
	if err != nil || strings.TrimSpace(p.APIKey) == "" {
		return convertproxy.Target{}, false
	}
	return convertproxy.Target{BaseURL: provider.OpenAIBaseURL(p.BaseFor(provider.ProtocolOpenAI)), Model: profileModel(target), ReasoningEffort: target.ReasoningEffort, APIKey: p.APIKey}, true
}

// resolveConversionKey finds the route a local key belongs to. It reads the
// stored state on every request, so an edit to a target Profile or Provider
// takes effect on the next request rather than after a restart.
func (u *UseCases) resolveConversionKey(key string) (convertproxy.Target, bool) {
	c, err := u.Conversion(context.Background())
	if err != nil {
		return convertproxy.Target{}, false
	}
	for _, agentID := range slices.Sorted(maps.Keys(c.Routes)) {
		p, err := u.providers.Get(converterRouteID(agentID))
		if err == nil && p.APIKey != "" && subtle.ConstantTimeCompare([]byte(key), []byte(p.APIKey)) == 1 {
			return u.conversionTarget(c.Routes[agentID])
		}
	}
	if shared := u.conversionAPIKey(); shared != "" && subtle.ConstantTimeCompare([]byte(key), []byte(shared)) == 1 {
		return u.conversionTarget(c.TargetProfile)
	}
	return convertproxy.Target{}, false
}

func (u *UseCases) proxyConfig(c ConversionConfig) convertproxy.Config {
	return convertproxy.Config{
		Enabled: c.Enabled,
		Listen:  c.Listen,
		Models:  []string{c.AnthropicModel, c.ResponsesModel, c.ChatModel},
		Resolve: u.resolveConversionKey,
	}
}

func (u *UseCases) startSavedConversion() error {
	c, err := u.Conversion(context.Background())
	if err != nil || !c.Enabled {
		return err
	}
	if err := validateConversionListen(c.Listen); err != nil {
		return err
	}
	return u.conversion.SetConfig(u.proxyConfig(c))
}

// conversionAgentProtocol is the format an Agent sends, which decides the
// alias its route Profile carries.
func conversionAgentProtocol(agentID string) (string, error) {
	if definition, ok := desktopapp.DefinitionFor(agentID); ok && definition.Protocol != "" {
		return definition.Protocol, nil
	}
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return "", err
	}
	agent, ok := manifest.Agents[agentID]
	if !ok {
		return "", oneerrors.New(oneerrors.InvalidRequest, "Unknown Agent: "+agentID)
	}
	return provider.ProtocolForAdapter(agent.ConfigAdapter), nil
}

func conversionAlias(c ConversionConfig, protocol string) string {
	switch protocol {
	case provider.ProtocolAnthropic:
		return c.AnthropicModel
	case provider.ProtocolResponses:
		return c.ResponsesModel
	}
	return c.ChatModel
}

// saveConverterEntry writes the Provider and Profile an Agent is pointed at:
// the adapter's address, a local key, and the alias the Agent is shown.
// Saving them rewrites every Agent already bound to them, which is how a new
// listen address or alias reaches the Agents' own files.
func (u *UseCases) saveConverterEntry(ctx context.Context, id, name, protocol, key, alias, listen string) error {
	origin := provider.AnthropicClientBaseURL("http://" + listen)
	entry := provider.Entry{ID: id, Name: name, BaseURL: provider.OpenAIBaseURL(origin), APIKey: key}
	if protocol == provider.ProtocolAnthropic {
		entry.AnthropicBaseURL = origin
	}
	if _, err := u.SaveProvider(ctx, entry, false, false); err != nil && !errors.Is(err, provider.ErrUnknownProvider) {
		return err
	}
	_, err := u.SaveProfile(ctx, SaveProfileOptions{ID: id, Label: name, Provider: id, Model: alias, Protocol: protocol, ConfigMode: "provider"})
	return err
}

// refreshConverterEntries carries the listen address and aliases to every
// converter entry that exists. The shared entries are only kept up to date,
// never created: new Agents get routes of their own.
func (u *UseCases) refreshConverterEntries(ctx context.Context, c ConversionConfig) error {
	shared := u.conversionAPIKey()
	for _, kind := range []string{"anthropic", "responses", "chat"} {
		id := converterPrefix + kind
		if _, err := u.providers.Get(id); err != nil || shared == "" {
			continue
		}
		protocol := map[string]string{"anthropic": provider.ProtocolAnthropic, "responses": provider.ProtocolResponses, "chat": provider.ProtocolOpenAI}[kind]
		if err := u.saveConverterEntry(ctx, id, "BootAgent Converter "+kind, protocol, shared, conversionAlias(c, protocol), c.Listen); err != nil {
			return err
		}
	}
	for _, agentID := range slices.Sorted(maps.Keys(c.Routes)) {
		id := converterRouteID(agentID)
		existing, err := u.providers.Get(id)
		if err != nil || existing.APIKey == "" {
			continue
		}
		protocol, err := conversionAgentProtocol(agentID)
		if err != nil {
			return err
		}
		if err := u.saveConverterEntry(ctx, id, "BootAgent "+agentID, protocol, existing.APIKey, conversionAlias(c, protocol), c.Listen); err != nil {
			return err
		}
	}
	return nil
}

// ensureConversionRoute gives an Agent its own route to `profileID` and starts
// the adapter. The route keeps its key across changes of target, so the
// Agent's own configuration only changes when it is first pointed at it.
func (u *UseCases) ensureConversionRoute(ctx context.Context, agentID, profileID string) (ConversionConfig, string, error) {
	protocol, err := conversionAgentProtocol(agentID)
	if err != nil {
		return ConversionConfig{}, "", err
	}
	if target := u.profileByID(profileID); target.ID == "" || target.Protocol != provider.ProtocolOpenAI {
		return ConversionConfig{}, "", oneerrors.New(oneerrors.InvalidRequest, "Compatibility mode forwards to a Chat Completions Profile")
	}
	c, err := u.Conversion(ctx)
	if err != nil {
		return c, "", err
	}
	c.Enabled = true
	c.Routes[agentID] = profileID
	id := converterRouteID(agentID)
	key := ""
	if existing, err := u.providers.Get(id); err == nil {
		key = existing.APIKey
	}
	if key == "" {
		if key, err = newConversionAPIKey(); err != nil {
			return c, "", err
		}
	}
	if c, err = u.saveConversion(ctx, c); err != nil {
		return c, "", err
	}
	if err := u.saveConverterEntry(ctx, id, "BootAgent "+agentID, protocol, key, conversionAlias(c, protocol), c.Listen); err != nil {
		return c, "", err
	}
	c.HasAPIKey = true
	return c, key, nil
}

// SaveConversion stores the adapter's own settings: whether it runs, where it
// listens and the aliases Agents are shown. Routes are not taken from the
// request -- they belong to the Agents pointed at them -- so a stale page
// cannot drop one.
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
	c.Routes = previous.Routes
	if strings.TrimSpace(c.TargetProfile) == "" {
		c.TargetProfile = previous.TargetProfile
	}
	paths, err := u.conversionSnapshotPaths(previous)
	if err != nil {
		return c, err
	}
	snapshots, err := snapshotManagedFiles(paths)
	if err != nil {
		return c, err
	}
	result, err := u.saveConversion(ctx, c)
	if err == nil {
		err = u.refreshConverterEntries(ctx, result)
	}
	if err != nil {
		return result, u.rollbackConversionFailure(ctx, err, previous, snapshots)
	}
	return result, nil
}

// conversionSnapshotPaths is what a change to compatibility mode can write:
// its own state, every converter entry, and on desktop platforms Claude
// Desktop's files and binding, which a converter entry may be reapplied to.
func (u *UseCases) conversionSnapshotPaths(c ConversionConfig, agents ...string) ([]managedFileSnapshot, error) {
	routed := slices.Collect(maps.Keys(c.Routes))
	if u.status.Platform.OS == "macos" || u.status.Platform.OS == "windows" {
		return u.conversionTransactionPaths(append(routed, agents...)...)
	}
	return u.conversionStatePaths(append(routed, agents...)...)
}

// saveConversion writes the settings and (re)starts the listener. It leaves
// the converter entries alone: a listener that cannot start fails here,
// before any Agent's files have been pointed at the new address.
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
	// Routes are not checked here. A route is checked when it is set, and a
	// request through one whose target has since gone is refused on its own
	// (conversionTarget). Checking every route on every save let one Agent's
	// leftover route block stopping the adapter for all of them.
	if c.Routes == nil {
		c.Routes = map[string]string{}
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
	b, _ := json.MarshalIndent(storedConversionConfig{Enabled: c.Enabled, Listen: c.Listen, TargetProfile: c.TargetProfile, Routes: c.Routes, AnthropicModel: c.AnthropicModel, ResponsesModel: c.ResponsesModel, ChatModel: c.ChatModel}, "", "  ")
	u.writeMu.Lock()
	_, err := u.filesystem.AtomicWrite(ctx, u.conversionPath(), append(b, '\n'), false)
	u.writeMu.Unlock()
	if err != nil {
		return c, err
	}
	if err := u.conversion.SetConfig(u.proxyConfig(c)); err != nil {
		return c, oneerrors.New(oneerrors.ConversionPortUnavailable, "The local protocol adapter could not listen on the configured port", oneerrors.WithStatus(409), oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	stored, err := u.Conversion(ctx)
	if err != nil {
		return c, err
	}
	return stored, nil
}

// conversionUsers is the Agents whose own files point at the adapter, each with
// the converter Profile it is bound to.
func (u *UseCases) conversionUsers() (map[string]string, error) {
	bindings, err := u.profiles.ListAgentBindings()
	if err != nil {
		return nil, err
	}
	users := map[string]string{}
	for agentID, binding := range bindings {
		if isConverterID(binding.ProfileRef) {
			users[agentID] = binding.ProfileRef
		}
	}
	return users, nil
}

// SettleConversion brings compatibility mode in line with the Agents that use
// it: the route of an Agent that has moved to an ordinary Profile is dropped,
// and the adapter stops once no Agent is pointed at it.
//
// Compatibility mode has no page of its own. It starts when an Agent needs it
// and this is how it ends, so nothing is left running, or left pointing at a
// Profile the user may now delete, after the last Agent has moved on.
//
// Callers must not hold conversionMu or writeMu.
func (u *UseCases) SettleConversion(ctx context.Context) error {
	if u == nil {
		return nil
	}
	u.conversionMu.Lock()
	defer u.conversionMu.Unlock()
	c, err := u.Conversion(ctx)
	if err != nil {
		return err
	}
	if !c.Enabled && len(c.Routes) == 0 {
		return nil
	}
	users, err := u.conversionUsers()
	if err != nil {
		return err
	}
	changed := false
	for agentID := range c.Routes {
		if users[agentID] != converterRouteID(agentID) {
			delete(c.Routes, agentID)
			changed = true
		}
	}
	if c.Enabled && len(users) == 0 {
		c.Enabled = false
		changed = true
	}
	if !changed {
		return nil
	}
	_, err = u.saveConversion(ctx, c)
	return err
}

// conversionTargetUsers is the Agents whose requests compatibility mode
// forwards to `profileID`. They are bound to a converter Profile, so the
// bindings alone do not show that they depend on this one. Callers must hold
// writeMu.
func (u *UseCases) conversionTargetUsers(ctx context.Context, profileID string, bindings map[string]profileStore.AgentBinding) []string {
	conversion, err := u.Conversion(ctx)
	if err != nil {
		return nil
	}
	var users []string
	for agentID, target := range conversion.Routes {
		if target != profileID {
			continue
		}
		if binding, err := u.profiles.ReadAgentBinding(agentID); err == nil && binding != nil && binding.ProfileRef == converterRouteID(agentID) {
			users = append(users, agentID)
		}
	}
	if conversion.TargetProfile == profileID {
		for agentID, binding := range bindings {
			if !slices.Contains(users, agentID) && isConverterID(binding.ProfileRef) {
				if _, routed := routeAgent(binding.ProfileRef); !routed {
					users = append(users, agentID)
				}
			}
		}
	}
	return users
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

// RegenerateConversionAPIKey gives every converter entry a new local key and
// rewrites the Agents pointed at them. Each route gets a key of its own; the
// shared entries keep sharing one.
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
	paths, err := u.conversionSnapshotPaths(previous)
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
	rotate := func(id, key string) error {
		entry, getErr := u.providers.Get(id)
		if getErr != nil {
			return nil
		}
		entry.APIKey = key
		_, saveErr := u.SaveProvider(ctx, entry, false, false)
		return saveErr
	}
	if u.conversionAPIKey() != "" {
		shared, keyErr := newConversionAPIKey()
		if keyErr != nil {
			return result, keyErr
		}
		for _, kind := range []string{"anthropic", "responses", "chat"} {
			if err = rotate(converterPrefix+kind, shared); err != nil {
				return result, err
			}
		}
	}
	for _, agentID := range slices.Sorted(maps.Keys(previous.Routes)) {
		key, keyErr := newConversionAPIKey()
		if keyErr != nil {
			return result, keyErr
		}
		if err = rotate(converterRouteID(agentID), key); err != nil {
			return result, err
		}
	}
	if result, err = u.saveConversion(ctx, previous); err != nil {
		return result, err
	}
	committed = true
	return result, nil
}
