package version

import "testing"

func TestUpdaterVersion(t *testing.T) {
	original := Version
	defer func() { Version = original }()

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "prefixed release", version: "v1.2.3", want: "1.2.3"},
		{name: "unprefixed release", version: "1.2.3", want: "1.2.3"},
		{name: "prefixed development", version: "v0.0.0-dev", want: ""},
		{name: "unprefixed development", version: "1.2.3-dev", want: ""},
		{name: "whitespace", version: "   ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Version = tt.version
			if got := UpdaterVersion(); got != tt.want {
				t.Fatalf("UpdaterVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}
