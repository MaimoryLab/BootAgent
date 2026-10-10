package app

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	configWriter "github.com/MaimoryLab/BootAgent/internal/config"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/modality"
	"github.com/MaimoryLab/BootAgent/internal/provider"
	"github.com/MaimoryLab/BootAgent/internal/skillgen"
)

// MultimodalDetection lists, for every Provider with a saved key, the models
// that offer a multimodal capability and where each finding came from.
type MultimodalDetection struct {
	Providers []MultimodalProvider `json:"providers"`
	// Installed are the generated Skills published to at least one Agent.
	Installed []InstalledMultimodal `json:"installed"`
	// Targets are the Agents a multimodal Skill can be installed into now.
	Targets []string `json:"targets"`
	// Generatable lists the adapters a Skill can be generated for in this
	// version; a capability on any other adapter is shown but not offered.
	Generatable []string `json:"generatable"`
	// Manual lists the capabilities a user can reach with a model ID typed by
	// hand, the route it is probed on, and how that probe is sent.
	Manual []MultimodalManualRoute `json:"manual"`
	// Fallback lists the Targets with no configurable environment: their key
	// goes to BootAgent's private fallback file, which the install says.
	Fallback []string `json:"fallback"`
	// FFmpeg says ffmpeg is on BootAgent's PATH. Video understanding needs it
	// where the Agent runs, which may differ, so this is a hint only.
	FFmpeg bool `json:"ffmpeg"`
	// NotesDir is where explore Skills keep what they learn, one
	// <provider>-<capability>.md per Skill; the install confirmation names it.
	NotesDir string `json:"notesDir"`
}

type MultimodalManualRoute struct {
	Capability string `json:"capability"`
	Adapter    string `json:"adapter"`
	Probe      string `json:"probe"`
}

type MultimodalProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// KeyEnv is the variable an install writes this Provider's key into, so
	// the install confirmation can name it.
	KeyEnv string `json:"keyEnv"`
	// Unsupported lists capabilities this Provider documents no API for.
	Unsupported []string `json:"unsupported"`
	// KeyFile is the private fallback file an install for an Agent without a
	// configurable environment writes the key into.
	KeyFile string `json:"keyFile"`
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
	Adapter    string `json:"adapter"`
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
	installTargets, eligible, err := u.multimodalInstallTargets()
	if err != nil {
		return MultimodalDetection{}, err
	}
	if installTargets == nil {
		installTargets = []string{}
	}
	fallback := []string{}
	for _, agentID := range installTargets {
		if !nativeSkillEnvAdapters[eligible[agentID].ConfigAdapter] {
			fallback = append(fallback, agentID)
		}
	}
	manual := []MultimodalManualRoute{}
	for _, capability := range modality.Capabilities {
		if adapter := modality.ManualAdapter(capability); adapter != "" {
			manual = append(manual, MultimodalManualRoute{Capability: capability, Adapter: adapter, Probe: modality.ProbeKind(adapter)})
		}
	}
	return MultimodalDetection{
		Providers: results, Installed: u.installedMultimodal(), Targets: installTargets,
		Generatable: skillgen.SupportedAdapters(), Manual: manual, Fallback: fallback, FFmpeg: u.commandOnPath("ffmpeg"),
		NotesDir: filepath.Join(u.status.Home, ".bootagent", "skill-notes"),
	}, nil
}

// commandOnPath reports whether a command resolves the way status does.
func (u *UseCases) commandOnPath(command string) bool {
	if u.status.Lookup != nil {
		_, ok := u.status.Lookup(command)
		return ok
	}
	_, ok := u.lookPath(command)
	return ok
}

func (u *UseCases) detectProvider(ctx context.Context, rules modality.Rules, target provider.Entry) MultimodalProvider {
	result := MultimodalProvider{ID: target.ID, Name: target.Name, KeyEnv: configWriter.SkillEnvName(target.ID), KeyFile: configWriter.SkillEnvFilePath(u.status.Home, target.ID), Unsupported: rules.Unsupported(target.ID), Models: []modality.Model{}}
	if result.Unsupported == nil {
		result.Unsupported = []string{}
	}
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

// ProbeMultimodal sends one live request testing a model's capability over an
// adapter (empty: the capability's chat route). It is a billed request, so
// only a button the user presses may call it; detection, saving and installing
// never do. A probe that produces billed output (one image) also needs
// confirmed, which the UI sets only after asking.
//
// A conclusive answer -- accepted, or refused for this model -- is cached and
// outranks every other source on the next detection. A rejected key or an
// unreachable endpoint says nothing about the model and is not cached.
func (u *UseCases) ProbeMultimodal(ctx context.Context, providerID, model, capability, adapter string, confirmed bool) (*MultimodalProbe, error) {
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
	if adapter == "" {
		adapter = modality.ChatAdapter(capability)
	}
	kind := modality.ProbeKind(adapter)
	if !modality.Serves(adapter, capability) || kind == modality.ProbeNone {
		return nil, oneerrors.New(oneerrors.InvalidRequest, "No live check is available for this capability: "+strings.TrimSpace(capability))
	}
	if kind == modality.ProbeConfirm && !confirmed {
		return nil, oneerrors.New(oneerrors.InvalidRequest, "This check produces billed output; confirm it first")
	}
	if adapter == modality.AdapterFramesVision {
		// Video by frame sampling is a chat vision request carrying frames:
		// probe that, and record it as what it is, image understanding.
		capability, adapter = modality.Vision, modality.AdapterChatVision
	}
	check := &MultimodalProbe{ProviderID: providerID, Model: model, Capability: capability, Adapter: adapter}
	target, err := u.providers.Get(providerID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(target.APIKey) == "" {
		check.Status, check.Reason = ModelCheckUnverified, "no_key"
		return check, nil
	}
	base := target.BaseURL
	var probe func(context.Context, string, string, string) (provider.ProbeResult, error)
	switch adapter {
	case modality.AdapterNovitaAudio:
		rules, err := modality.Load()
		if err != nil {
			return nil, err
		}
		if base = rules.NativeBaseURL(target.ID); base == "" {
			return nil, oneerrors.New(oneerrors.InvalidRequest, "This Provider has no native speech route")
		}
		probe = u.provider.ProbeNativeSpeech
	case modality.AdapterChatVision:
		probe = u.provider.ProbeVision
	case modality.AdapterAudioTranscriptions:
		probe = u.provider.ProbeTranscription
	case modality.AdapterAudioSpeech:
		probe = u.provider.ProbeSpeech
	case modality.AdapterImages:
		probe = u.provider.ProbeImageGeneration
	default:
		return nil, oneerrors.New(oneerrors.InvalidRequest, "No live check is available for "+adapter)
	}
	result, err := probe(ctx, target.APIKey, model, base)
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
