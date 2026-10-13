package binding

import (
	"context"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/app"
)

func TestSkillPreviewImportTreatsAClosedPickerAsNothingChosen(t *testing.T) {
	service := &SkillService{core: &app.UseCases{}, selectPath: func(string) (string, error) { return "", nil }}
	preview, err := service.PreviewImport(context.Background(), app.SkillImportRequest{Source: "zip"})
	if err != nil {
		t.Fatalf("closing the picker should not be an error: %v", err)
	}
	if preview.Token != "" || len(preview.Candidates) != 0 {
		t.Fatalf("closing the picker should return an empty preview, got %+v", preview)
	}
}
