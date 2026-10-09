package app

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	"github.com/MaimoryLab/BootAgent/internal/desktopapp"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	profileStore "github.com/MaimoryLab/BootAgent/internal/profile"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

// ProfileModelCheck is the verdict of a live request to a Profile's model,
// sent only when the user asks for it.
//
// A model the provider lists can still be refused when an Agent calls it --
// not served over that Agent's protocol, or not granted to the key -- and a
// Profile edit rewrites every Agent bound to it at once. The check lets the
// user find that out before an Agent fails. It is not run on save or apply:
// each check is a billed completion request, so it is the user's to send.
type ProfileModelCheck struct {
	ProfileID string `json:"profileId"`
	Model     string `json:"model"`
	// Status is "available" when every checked Agent's protocol answered,
	// "unavailable" when the provider answered and refused, and "unverified"
	// when no verdict could be had at all. The last is not a claim that the
	// model is broken, and the UI must not word it as one.
	Status string `json:"status"`
	// Reason is the cause the remedy follows from:
	//   refused   the provider refused this model on this protocol
	//   key       the provider rejected the API key
	//   transient the provider answered but was busy or failing (408, 429, 5xx)
	//   network   the provider could not be reached
	//   no_key    no API key is saved for the Provider
	//   error     the request could not be made (an endpoint misconfiguration)
	// Empty when Status is "available".
	Reason   string `json:"reason,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	// HTTPStatus is the status the provider answered with, 0 when it did not.
	HTTPStatus int `json:"httpStatus,omitempty"`
	// Detail is the provider's own explanation, cleaned and bounded.
	Detail string `json:"detail,omitempty"`
	// Agents are the Agents the verdict is about: on a failure, those whose
	// requests to this model would fail; when available, those checked. Empty
	// for a Profile nothing follows yet.
	Agents []string `json:"agents"`
}

const (
	ModelCheckAvailable   = "available"
	ModelCheckUnavailable = "unavailable"
	ModelCheckUnverified  = "unverified"
)

// CheckProfileModel sends one live request per protocol the Profile's Agents
// use and reports the verdict. agentIDs narrows the check to those Agents --
// the one a page is configuring, even before it follows the Profile; empty
// means every Agent bound to the Profile.
func (u *UseCases) CheckProfileModel(ctx context.Context, profileID string, agentIDs []string) (*ProfileModelCheck, error) {
	if u == nil || u.provider == nil {
		return nil, oneerrors.New(oneerrors.InternalError, "Profile service is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Profile request was cancelled"); err != nil {
		return nil, err
	}
	profile := u.profileByID(profileID)
	if profile.ID == "" {
		return nil, oneerrors.New(oneerrors.InvalidRequest, "Profile not found: "+strings.TrimSpace(profileID))
	}
	// The local protocol adapter's own Profiles point at the adapter on
	// 127.0.0.1, which forwards the probe upstream as a billed request whose
	// verdict the adapter already established when it was enabled.
	if isConverterID(profile.ID) {
		return nil, oneerrors.New(oneerrors.InvalidRequest, "The protocol adapter's Profile is checked when the adapter is enabled")
	}
	if profileModel(profile) == "" {
		return nil, oneerrors.New(oneerrors.InvalidRequest, "Profile has no model to check")
	}
	agents := slices.Clone(agentIDs)
	if len(agents) == 0 {
		bindings, err := u.profiles.ListAgentBindings()
		if err != nil {
			return nil, err
		}
		for agentID, binding := range bindings {
			if binding.ProfileRef == profile.ID {
				agents = append(agents, agentID)
			}
		}
	}
	check := u.checkProfileModel(ctx, profile, agents)
	if check == nil {
		// Cancelled, not answered: saying "unverified" would blame the network
		// for the user closing the window.
		return nil, contextError(ctx, "Profile request was cancelled")
	}
	return check, nil
}

// checkProfileModel returns nil only when ctx was cancelled mid-check.
func (u *UseCases) checkProfileModel(ctx context.Context, profile profileStore.Profile, agentIDs []string) *ProfileModelCheck {
	model := profileModel(profile)
	sort.Strings(agentIDs)
	agentIDs = slices.Compact(agentIDs)
	check := &ProfileModelCheck{ProfileID: profile.ID, Model: model, Agents: []string{}}

	target, err := u.providers.Get(profile.Provider)
	if err != nil {
		check.Status, check.Reason, check.Agents = ModelCheckUnverified, "error", agentIDs
		check.Detail = oneerrors.As(err).Message
		return check
	}
	if strings.TrimSpace(target.APIKey) == "" {
		check.Status, check.Reason, check.Agents = ModelCheckUnverified, "no_key", agentIDs
		return check
	}

	options := map[string][]string{}
	needed := map[string]bool{}
	for _, agentID := range agentIDs {
		accepted := u.agentProtocolOptions(agentID, profile)
		if len(accepted) == 0 {
			// An Agent BootAgent does not configure has no request to check.
			continue
		}
		options[agentID] = accepted
		for _, protocolID := range accepted {
			needed[protocolID] = true
		}
	}
	// A Profile nothing follows yet is still about to be applied; check the
	// protocol it is set up for, so the first Agent to use it is not the first
	// to find out.
	unbound := len(options) == 0
	if unbound {
		protocolID := profile.Protocol
		if protocolID == "" {
			protocolID = provider.ProtocolOpenAI
		}
		needed[protocolID] = true
		options[""] = []string{protocolID}
	}
	protocols := make([]string, 0, len(needed))
	for protocolID := range needed {
		protocols = append(protocols, protocolID)
	}
	sort.Strings(protocols)

	results, err := u.probeProtocols(ctx, protocols, target.APIKey, model, target.BaseFor)
	if ctx.Err() != nil {
		// Cancelled, not answered: saying "unverified" would blame the network
		// for the user closing the window.
		return nil
	}
	if err != nil {
		check.Status, check.Reason = ModelCheckUnverified, "error"
		check.Detail = oneerrors.As(err).Message
		check.Agents = agentIDs
		return check
	}

	var worst *provider.ProbeResult
	worstProtocol := ""
	for agentID, accepted := range options {
		agentOK := false
		var agentWorst *provider.ProbeResult
		agentProtocol := ""
		for _, protocolID := range accepted {
			result, ok := results[protocolID]
			if !ok {
				continue
			}
			if result.OK {
				agentOK = true
				break
			}
			if agentWorst == nil || modelCheckRank(result) > modelCheckRank(*agentWorst) {
				candidate := result
				agentWorst, agentProtocol = &candidate, protocolID
			}
		}
		if agentOK {
			continue
		}
		if agentWorst == nil {
			// Every probe for this Agent errored before a verdict.
			unknown := provider.ProbeResult{}
			agentWorst = &unknown
			agentProtocol = accepted[0]
		}
		if agentID != "" {
			check.Agents = append(check.Agents, agentID)
		}
		if worst == nil || modelCheckRank(*agentWorst) > modelCheckRank(*worst) {
			worst, worstProtocol = agentWorst, agentProtocol
		}
	}
	if worst == nil {
		check.Status = ModelCheckAvailable
		for agentID := range options {
			if agentID != "" {
				check.Agents = append(check.Agents, agentID)
			}
		}
		sort.Strings(check.Agents)
		if len(protocols) == 1 {
			check.Protocol = protocols[0]
		}
		return check
	}
	sort.Strings(check.Agents)
	check.Protocol = worstProtocol
	check.HTTPStatus = worst.Status
	check.Detail = worst.Detail
	switch {
	case !worst.Reachable && worst.Status == 0 && worst.ErrorCode == nil && worst.Message == "":
		check.Status, check.Reason = ModelCheckUnverified, "error"
	case !worst.Reachable:
		check.Status, check.Reason = ModelCheckUnverified, "network"
		check.HTTPStatus = 0
	case worst.ErrorCode != nil && *worst.ErrorCode == oneerrors.APIKeyRejected:
		check.Status, check.Reason = ModelCheckUnavailable, "key"
	case transientProbe(*worst):
		check.Status, check.Reason = ModelCheckUnverified, "transient"
	default:
		check.Status, check.Reason = ModelCheckUnavailable, "refused"
	}
	if check.Detail == "" && worst.Message != "" && check.Reason != "refused" {
		check.Detail = worst.Message
	}
	return check
}

// modelCheckRank orders failures by how much the user can do about them: a
// refusal names the model, a rejected key names the credential, and a busy or
// unreachable endpoint says nothing about either.
func modelCheckRank(result provider.ProbeResult) int {
	switch {
	case !result.Reachable:
		return 1
	case transientProbe(result):
		return 2
	case result.ErrorCode != nil && *result.ErrorCode == oneerrors.APIKeyRejected:
		return 3
	default:
		return 4
	}
}

// transientProbe reports an answer that is about the provider's state, not
// the model: a timeout, rate limiting, or a server error. Calling those
// "unavailable" would send the user to abandon a model that works -- and
// importing several Profiles at once is exactly what draws a 429. A 5xx the
// probe already read as "this endpoint does not serve the protocol" is a real
// refusal and stays one.
func transientProbe(result provider.ProbeResult) bool {
	if !result.Reachable {
		return false
	}
	if result.ErrorCode != nil && *result.ErrorCode == oneerrors.ProtocolUnsupported {
		return false
	}
	return result.Status == 408 || result.Status == 429 || result.Status >= 500
}

// agentProtocolOptions lists the protocols an Agent's request to this Profile's
// model can travel over; the Agent works if any of them does. Empty means
// BootAgent writes no model into that Agent, so there is nothing to check.
func (u *UseCases) agentProtocolOptions(agentID string, profile profileStore.Profile) []string {
	if definition, ok := desktopapp.DefinitionFor(agentID); ok {
		if definition.SharedConfigAgentID != "" {
			manifest, err := catalog.LoadEmbedded()
			if err != nil {
				return nil
			}
			shared, exists := manifest.Agents[definition.SharedConfigAgentID]
			if !exists {
				return nil
			}
			return adapterProtocolOptions(shared.ConfigAdapter)
		}
		if agentID == desktopapp.ClaudeDesktopID && profile.Protocol == provider.ProtocolOpenAI {
			// A Chat Completions Profile reaches Claude Desktop through local
			// protocol adaptation, which calls the upstream over Chat Completions.
			return []string{provider.ProtocolOpenAI}
		}
		if definition.Protocol != "" {
			return []string{definition.Protocol}
		}
		if definition.ConfigAdapter != "" {
			return adapterProtocolOptions(definition.ConfigAdapter)
		}
		return nil
	}
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return nil
	}
	agent, ok := manifest.Agents[agentID]
	if !ok || agent.ConfigAdapter == "" {
		return nil
	}
	return adapterProtocolOptions(agent.ConfigAdapter)
}

// adapterProtocolOptions is the protocol a Profile edit or Apply writes for an
// adapter. It has to match the write exactly: checking a protocol the write
// does not use passes a model the Agent then cannot call.
//
// dsh is the case that makes this matter. Its pi-ai route can speak either
// OpenAI wire protocol, and the install probe accepts either, but
// writeManagedAgentConfig is only told which one by the install path; a
// Profile save and an Apply always write Chat Completions. Accepting Responses
// here passed a Responses-only model while dsh was rewritten to completions.
func adapterProtocolOptions(adapter string) []string {
	return []string{provider.ProtocolForAdapter(adapter)}
}
