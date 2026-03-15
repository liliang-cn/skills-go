package skill

import "testing"

func TestValidateMeta(t *testing.T) {
	tests := []struct {
		name      string
		meta      *Meta
		skillPath string
		wantError bool
	}{
		{
			name: "valid metadata",
			meta: &Meta{
				Name:        "valid-skill",
				Description: "Use when validating skill metadata.",
				Metadata: map[string]interface{}{
					"author": "example",
				},
			},
			skillPath: "/tmp/valid-skill",
		},
		{
			name: "missing description",
			meta: &Meta{
				Name: "valid-skill",
			},
			skillPath: "/tmp/valid-skill",
			wantError: true,
		},
		{
			name: "invalid uppercase name",
			meta: &Meta{
				Name:        "Invalid-Skill",
				Description: "desc",
			},
			skillPath: "/tmp/Invalid-Skill",
			wantError: true,
		},
		{
			name: "directory mismatch",
			meta: &Meta{
				Name:        "valid-skill",
				Description: "desc",
			},
			skillPath: "/tmp/other-dir",
			wantError: true,
		},
		{
			name: "metadata values must be strings",
			meta: &Meta{
				Name:        "valid-skill",
				Description: "desc",
				Metadata: map[string]interface{}{
					"version": 1,
				},
			},
			skillPath: "/tmp/valid-skill",
			wantError: true,
		},
		{
			name: "compatibility length enforced",
			meta: &Meta{
				Name:          "valid-skill",
				Description:   "desc",
				Compatibility: string(make([]byte, 501)),
			},
			skillPath: "/tmp/valid-skill",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMeta(tt.meta, tt.skillPath)
			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
