package binding

import (
	"context"

	"github.com/MaimoryLab/BootAgent/internal/app"
)

// MultimodalService exposes multimodal capability detection and install.
// Detect is free -- it lists models -- so pages may call it on open. Probe is a
// billed request and may only be called from a button the user presses.
// Uninstall goes through SkillService.Uninstall, which also removes the key
// variable once no Skill needs it.
type MultimodalService struct {
	core *app.UseCases
}

func NewMultimodalService(core *app.UseCases) *MultimodalService {
	return &MultimodalService{core: core}
}

type MultimodalProbeRequest struct {
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Capability string `json:"capability"`
	// Adapter picks the route; empty is the capability's chat route. A model
	// the user typed by hand is probed on its manual route.
	Adapter string `json:"adapter"`
	// Confirmed must be set for a probe that produces billed output (one
	// image); the UI sets it only after asking the user.
	Confirmed bool `json:"confirmed"`
}

func (s *MultimodalService) Detect(ctx context.Context) (app.MultimodalDetection, error) {
	if err := contextError(ctx); err != nil {
		return app.MultimodalDetection{}, err
	}
	if s == nil || s.core == nil {
		return app.MultimodalDetection{}, notReady("Multimodal detection is not configured")
	}
	return s.core.DetectMultimodal(ctx)
}

func (s *MultimodalService) Probe(ctx context.Context, request MultimodalProbeRequest) (*app.MultimodalProbe, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.core == nil {
		return nil, notReady("Multimodal probing is not configured")
	}
	return s.core.ProbeMultimodal(ctx, request.Provider, request.Model, request.Capability, request.Adapter, request.Confirmed)
}

// Install generates the Skill and publishes it to the chosen Agents, writing
// the Provider key into each Agent's own configuration. It sends no billed
// request.
func (s *MultimodalService) Install(ctx context.Context, request app.MultimodalInstallRequest) (app.MultimodalInstallResult, error) {
	if err := contextError(ctx); err != nil {
		return app.MultimodalInstallResult{}, err
	}
	if s == nil || s.core == nil {
		return app.MultimodalInstallResult{}, notReady("Multimodal install is not configured")
	}
	return s.core.InstallMultimodal(ctx, request)
}
