package version

import "testing"

func TestInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		commit  string
		date    string
		want    string
	}{
		{
			name:    "all fields present",
			version: "1.2.3",
			commit:  "abc1234",
			date:    "2026-07-04T00:00:00Z",
			want:    "argus 1.2.3 (commit abc1234, built 2026-07-04T00:00:00Z)",
		},
		{
			name:    "all empty falls back to dev build",
			version: "",
			commit:  "",
			date:    "",
			want:    "argus dev (commit none, built unknown)",
		},
		{
			name:    "empty commit only",
			version: "1.0.0",
			commit:  "",
			date:    "2026-01-01T00:00:00Z",
			want:    "argus 1.0.0 (commit none, built 2026-01-01T00:00:00Z)",
		},
		{
			name:    "empty version only",
			version: "",
			commit:  "deadbee",
			date:    "2026-01-01T00:00:00Z",
			want:    "argus dev (commit deadbee, built 2026-01-01T00:00:00Z)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Info(tt.version, tt.commit, tt.date); got != tt.want {
				t.Errorf("Info(%q, %q, %q) = %q, want %q",
					tt.version, tt.commit, tt.date, got, tt.want)
			}
		})
	}
}

func TestString_UsesPackageVars(t *testing.T) {
	// String() must route through Info() and never return an empty string,
	// even for a fully unstamped ("dev") build.
	if got := String(); got == "" {
		t.Fatal("String() returned an empty string")
	}
}
