package binding

import (
	"context"

	"github.com/MaimoryLab/BootAgent/internal/app"
)

// MultimodalService exposes multimodal capability detection. Detect is free --
// it lists models -- so pages may call it on open. Probe is a billed request
// and may only be called from a button the user presses.
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
	return s.core.ProbeMultimodal(ctx, request.Provider, request.Model, request.Capability)
}
