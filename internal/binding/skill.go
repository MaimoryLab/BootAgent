package binding

import (
	"context"
	"errors"
	"strings"

	"github.com/MaimoryLab/OneAgent/internal/app"
)

// SkillService is the narrow Wails boundary for the local Skills registry.
// Source paths are selected here and never cross into renderer state.
type SkillService struct {
	core       *app.UseCases
	selectPath func(source string) (string, error)
}

func NewSkillService(core *app.UseCases) *SkillService {
	return &SkillService{core: core}
}

func (s *SkillService) List(ctx context.Context) ([]app.SkillSummary, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.core == nil {
		return nil, notReady("Skill service is not configured")
	}
	return s.core.ListSkills(ctx)
}

func (s *SkillService) Scan(ctx context.Context) (app.SkillScanResult, error) {
	if err := contextError(ctx); err != nil {
		return app.SkillScanResult{}, err
	}
	if s == nil || s.core == nil {
		return app.SkillScanResult{}, notReady("Skill service is not configured")
	}
	return s.core.ScanSkills(ctx)
}

func (s *SkillService) Get(ctx context.Context, id string) (app.SkillSummary, error) {
	if err := contextError(ctx); err != nil {
		return app.SkillSummary{}, err
	}
	if s == nil || s.core == nil {
		return app.SkillSummary{}, notReady("Skill service is not configured")
	}
	return s.core.GetSkill(ctx, id)
}

func (s *SkillService) PreviewImport(ctx context.Context, request app.SkillImportRequest) (app.SkillImportPreview, error) {
	if err := contextError(ctx); err != nil {
		return app.SkillImportPreview{}, err
	}
	if s == nil || s.core == nil {
		return app.SkillImportPreview{}, notReady("Skill service is not configured")
	}
	request.Source = strings.ToLower(strings.TrimSpace(request.Source))
	if request.Source != "agent" && request.Source != "folder" && request.Source != "zip" {
		return app.SkillImportPreview{}, errors.New("invalid Skill import source")
	}
	selector := s.selectPath
	if selector == nil {
		selector = selectSkillPath
	}
	path, err := selector(request.Source)
	if err != nil {
		return app.SkillImportPreview{}, err
	}
	if path == "" {
		return app.SkillImportPreview{}, errors.New("Skill import source selection cancelled")
	}
	return s.core.PreviewSkillImport(ctx, request, path)
}

func (s *SkillService) Apply(ctx context.Context, request app.SkillApplyRequest) (app.SkillApplyResult, error) {
	if err := contextError(ctx); err != nil {
		return app.SkillApplyResult{}, err
	}
	if s == nil || s.core == nil {
		return app.SkillApplyResult{}, notReady("Skill service is not configured")
	}
	return s.core.ApplySkills(ctx, request), nil
}

func (s *SkillService) Uninstall(ctx context.Context, id string) (app.SkillUninstallResult, error) {
	if err := contextError(ctx); err != nil {
		return app.SkillUninstallResult{}, err
	}
	if s == nil || s.core == nil {
		return app.SkillUninstallResult{}, notReady("Skill service is not configured")
	}
	return s.core.UninstallSkill(ctx, id), nil
}

func (s *SkillService) ListBackups(ctx context.Context) ([]app.SkillBackupSummary, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.core == nil {
		return nil, notReady("Skill service is not configured")
	}
	return s.core.ListSkillBackups(ctx)
}

func (s *SkillService) RestoreBackup(ctx context.Context, backupID string, targets []string) (app.SkillApplyResult, error) {
	if err := contextError(ctx); err != nil {
		return app.SkillApplyResult{}, err
	}
	if s == nil || s.core == nil {
		return app.SkillApplyResult{}, notReady("Skill service is not configured")
	}
	return s.core.RestoreSkillBackup(ctx, backupID, targets), nil
}

func (s *SkillService) SetDraftState(ctx context.Context, dirty bool, locale string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.core == nil {
		return notReady("Skill service is not configured")
	}
	s.core.SetSkillDraftState(dirty, locale)
	return nil
}
