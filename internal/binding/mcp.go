package binding

import (
	"context"

	"github.com/MaimoryLab/OneAgent/internal/app"
	"github.com/MaimoryLab/OneAgent/internal/mcp"
)

type MCPExportRequest struct {
	Mode             string   `json:"mode"`
	Password         string   `json:"password,omitempty"`
	ConfirmPlaintext bool     `json:"confirm_plaintext,omitempty"`
	ServerIDs        []string `json:"server_ids,omitempty"`
}

type MCPImportRequest struct {
	Data     string `json:"data"`
	Password string `json:"password,omitempty"`
}

type MCPService struct{ core *app.UseCases }

func NewMCPService(core *app.UseCases) *MCPService { return &MCPService{core: core} }

func (s *MCPService) List(ctx context.Context) ([]app.MCPServerSummary, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.core == nil {
		return nil, notReady("MCP service is not configured")
	}
	return s.core.ListMCP(ctx)
}

func (s *MCPService) Scan(ctx context.Context) (app.MCPScanResult, error) {
	if err := contextError(ctx); err != nil {
		return app.MCPScanResult{}, err
	}
	if s == nil || s.core == nil {
		return app.MCPScanResult{}, notReady("MCP service is not configured")
	}
	return s.core.ScanMCP(ctx)
}

func (s *MCPService) Get(ctx context.Context, id, sourceAgent string) (app.MCPServerDetail, error) {
	if err := contextError(ctx); err != nil {
		return app.MCPServerDetail{}, err
	}
	if s == nil || s.core == nil {
		return app.MCPServerDetail{}, notReady("MCP service is not configured")
	}
	return s.core.GetMCP(ctx, id, sourceAgent)
}

func (s *MCPService) Apply(ctx context.Context, request app.MCPApplyRequest) (app.MCPApplyResult, error) {
	if err := contextError(ctx); err != nil {
		return app.MCPApplyResult{}, err
	}
	if s == nil || s.core == nil {
		return app.MCPApplyResult{}, notReady("MCP service is not configured")
	}
	return s.core.ApplyMCP(ctx, request), nil
}

func (s *MCPService) Export(ctx context.Context, request MCPExportRequest) (string, error) {
	if err := contextError(ctx); err != nil {
		return "", err
	}
	if s == nil || s.core == nil {
		return "", notReady("MCP service is not configured")
	}
	data, err := s.core.ExportMCP(ctx, mcp.ExportOptions{Mode: mcp.SecretMode(request.Mode), Password: request.Password, ConfirmPlaintext: request.ConfirmPlaintext, ServerIDs: request.ServerIDs})
	return string(data), err
}

func (s *MCPService) SaveImported(ctx context.Context, registry mcp.Registry) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.core == nil {
		return notReady("MCP service is not configured")
	}
	return s.core.SaveImportedMCP(ctx, registry)
}

func (s *MCPService) PreviewImport(ctx context.Context, request MCPImportRequest) (mcp.Registry, error) {
	if err := contextError(ctx); err != nil {
		return mcp.Registry{}, err
	}
	if s == nil || s.core == nil {
		return mcp.Registry{}, notReady("MCP service is not configured")
	}
	return s.core.ImportMCP(ctx, []byte(request.Data), request.Password)
}

func (s *MCPService) SetDraftState(ctx context.Context, dirty bool, locale string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.core == nil {
		return notReady("MCP service is not configured")
	}
	s.core.SetMCPDraftState(dirty, locale)
	return nil
}
