package binding

import (
	"context"
	"reflect"
	"sort"
	"testing"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
)

func TestServiceMethodAllowlist(t *testing.T) {
	tests := []struct {
		service any
		want    []string
	}{
		{&StatusService{}, []string{"GetStatus"}},
		{&ProviderService{}, []string{"ListModels", "ListProviders", "OpenRegistration", "Probe"}},
		{&AgentService{}, []string{"Activate", "Install"}},
		{&ProfileService{}, []string{"ListProfiles", "SaveProfile"}},
	}
	for _, test := range tests {
		typeOf := reflect.TypeOf(test.service)
		got := make([]string, 0, typeOf.NumMethod())
		for index := 0; index < typeOf.NumMethod(); index++ {
			got = append(got, typeOf.Method(index).Name)
		}
		sort.Strings(got)
		sort.Strings(test.want)
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s methods = %v, want %v", typeOf, got, test.want)
		}
	}
}

func TestOpenRegistrationUsesCatalogURLOnly(t *testing.T) {
	var opened string
	service := &ProviderService{opener: func(value string) error {
		opened = value
		return nil
	}}
	response, err := service.OpenRegistration(context.Background(), OpenRegistrationRequest{Provider: "ppio"})
	if err != nil {
		t.Fatal(err)
	}
	if opened != "https://ppio.com/" || response.URL != opened {
		t.Fatalf("unexpected registration URL: opened=%q response=%#v", opened, response)
	}

	_, err = service.OpenRegistration(context.Background(), OpenRegistrationRequest{Provider: "https://example.com"})
	if err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
		t.Fatalf("arbitrary URL was not rejected: %v", err)
	}
}

func TestServiceCancellationUsesStableTimeoutCode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (&ProviderService{}).ListProviders(ctx)
	if err == nil || oneerrors.As(err).Code != oneerrors.Timeout {
		t.Fatalf("cancellation error = %v", err)
	}
}
