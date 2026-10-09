package app

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/modality"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

// MultimodalDetection lists, for every Provider with a saved key, the models
// that offer a multimodal capability and where each finding came from.
type MultimodalDetection struct {
	Providers []MultimodalProvider `json:"providers"`
}

type MultimodalProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Listed is false when /models could not be read. Message and ErrorCode
	// then say why; catalog generation models are still listed, since they do
	// not come from /models.
	Listed    bool             `json:"listed"`
	Message   string           `json:"message,omitempty"`
	ErrorCode string           `json:"errorCode,omitempty"`
	Models    []modality.Model `json:"models"`
}

// MultimodalProbe is the verdict of one capability probe. Status and Reason
// use the Profile model check's vocabulary; see ProfileModelCheck.
type MultimodalProbe struct {
	ProviderID string `json:"providerId"`
	Model      string `json:"model"`
	Capability string `json:"capability"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// DetectMultimodal lists each keyed Provider's models and resolves their
// capabilities. Listing models is an unbilled GET, so opening the Skills page
// may call this; it never sends a probe.
func (u *UseCases) DetectMultimodal(ctx context.Context) (MultimodalDetection, error) {
	if u == nil || u.provider == nil {
		return MultimodalDetection{}, oneerrors.New(oneerrors.InternalError, "Multimodal detection is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Multimodal detection was cancelled"); err != nil {
		return MultimodalDetection{}, err
	}
	rules, err := modality.Load()
	if err != nil {
		return MultimodalDetection{}, err
	}
	public, err := u.providers.Public()
	if err != nil {
		return MultimodalDetection{}, err
	}
	var targets []provider.Entry
	for id, meta := range public {
		if !meta.HasKey {
			continue
		}
		entry, err := u.providers.Get(id)
		if err != nil || strings.TrimSpace(entry.APIKey) == "" {
			continue
		}
		targets = append(targets, entry)
	}
	sort.Slice(targets, func(i, j int) bool {
		left, right := catalog.ProviderOrder(targets[i].ID), catalog.ProviderOrder(targets[j].ID)
		// Built-in Providers keep the catalog order; custom ones (order 0)
		// follow, by ID.
		if (left == 0) != (right == 0) {
			return left != 0
		}
		if left != right {
			return left < right
		}
		return targets[i].ID < targets[j].ID
	})

	results := make([]MultimodalProvider, len(targets))
	var group sync.WaitGroup
	for index, target := range targets {
		group.Add(1)
		go func(index int, target provider.Entry) {
			defer group.Done()
			results[index] = u.detectProvider(ctx, rules, target)
		}(index, target)
	}
	group.Wait()
	if err := contextError(ctx, "Multimodal detection was cancelled"); err != nil {
		return MultimodalDetection{}, err
	}
	return MultimodalDetection{Providers: results}, nil
}

func (u *UseCases) detectProvider(ctx context.Context, rules modality.Rules, target provider.Entry) MultimodalProvider {
	result := MultimodalProvider{ID: target.ID, Name: target.Name, Models: []modality.Model{}}
	listing, err := u.provider.ListModels(ctx, "custom", target.APIKey, target.BaseURL)
	var entries []provider.ModelEntry
	switch {
	case err != nil:
		result.Message = oneerrors.As(err).Message
		result.ErrorCode = oneerrors.As(err).Code
	case !listing.OK:
		result.Message = listing.Message
		if listing.ErrorCode != nil {
			result.ErrorCode = *listing.ErrorCode
		}
	default:
		result.Listed = true
		entries = listing.Entries
	}
	models := rules.Resolve(target.ID, entries, u.capabilities.Modalities(target))
	if models != nil {
		result.Models = models
	}
	return result
}

// ProbeMultimodal sends one live request testing a model's capability. It is a
// billed request, so only a button the user presses may call it; detection,
// saving and installing never do. A conclusive answer -- accepted, or refused
// for this model -- is cached and outranks every other source on the next
// detection. A rejected key or an unreachable endpoint says nothing about the
// model and is not cached.
func (u *UseCases) ProbeMultimodal(ctx context.Context, providerID, model, capability string) (*MultimodalProbe, error) {
	if u == nil || u.provider == nil {
		return nil, oneerrors.New(oneerrors.InternalError, "Multimodal probing is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "Multimodal probe was cancelled"); err != nil {
		return nil, err
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, oneerrors.New(oneerrors.InvalidRequest, "A model is required")
	}
	adapter := modality.ChatAdapter(capability)
	// Only probes that cost a token or so can be sent from here. A probe that
	// produces billed output needs its own confirmation path, and the rest do
	// not exist yet.
	if modality.ProbeKind(adapter) != modality.ProbeMinimal {
		return nil, oneerrors.New(oneerrors.InvalidRequest, "No live check is available for this capability: "+strings.TrimSpace(capability))
	}
	check := &MultimodalProbe{ProviderID: providerID, Model: model, Capability: capability}
	target, err := u.providers.Get(providerID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(target.APIKey) == "" {
		check.Status, check.Reason = ModelCheckUnverified, "no_key"
		return check, nil
	}
	result, err := u.provider.ProbeVision(ctx, target.APIKey, model, target.BaseURL)
	if ctx.Err() != nil {
		return nil, contextError(ctx, "Multimodal probe was cancelled")
	}
	if err != nil {
		check.Status, check.Reason, check.Detail = ModelCheckUnverified, "error", oneerrors.As(err).Message
		return check, nil
	}
	if result.OK {
		check.Status = ModelCheckAvailable
	} else {
		check.Status, check.Reason, check.HTTPStatus, check.Detail = probeVerdict(result)
	}
	if check.Status == ModelCheckAvailable || check.Reason == "refused" {
		u.capabilityMu.Lock()
		// Derived data: a failed cache write must not turn a verdict the user
		// paid for into an error.
		_ = u.capabilities.SaveModality(ctx, target, model, capability, provider.ModalityVerdict{
			Supported: check.Status == ModelCheckAvailable,
			Source:    modality.SourceProbe,
			Adapter:   adapter,
			Status:    result.Status,
		})
		u.capabilityMu.Unlock()
	}
	return check, nil
}
