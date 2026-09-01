package binding

import (
	"context"
	"errors"
	"testing"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

func TestMarketplaceOpenExternalAllowsHTTPS(t *testing.T) {
	var opened string
	service := &MarketplaceService{opener: func(value string) error {
		opened = value
		return nil
	}}

	if err := service.OpenExternal(context.Background(), OpenExternalRequest{URL: "https://example.com/docs?q=1#readme"}); err != nil {
		t.Fatal(err)
	}
	if opened != "https://example.com/docs?q=1#readme" {
		t.Fatalf("opened %q", opened)
	}
}

func TestMarketplaceOpenExternalRejectsUnsafeURLs(t *testing.T) {
	service := &MarketplaceService{opener: func(string) error { return nil }}
	for _, value := range []string{
		"javascript:alert(1)",
		"file:///tmp/secret",
		"https://user:password@example.com/",
		"https://localhost/admin",
		"https://127.0.0.1/admin",
		"https://[fe80::1%25en0]/admin",
	} {
		err := service.OpenExternal(context.Background(), OpenExternalRequest{URL: value})
		if err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
			t.Errorf("OpenExternal(%q) error = %v", value, err)
		}
	}
}

func TestMarketplaceOpenExternalSurfacesBrowserFailure(t *testing.T) {
	service := &MarketplaceService{opener: func(string) error { return errors.New("no browser") }}
	err := service.OpenExternal(context.Background(), OpenExternalRequest{URL: "https://example.com"})
	if err == nil || oneerrors.As(err).Code != oneerrors.InternalError || !oneerrors.As(err).Retryable {
		t.Fatalf("unexpected error: %v", err)
	}
}
