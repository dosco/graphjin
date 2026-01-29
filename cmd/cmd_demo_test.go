package main

import "testing"

func TestParseDBFlags_Empty(t *testing.T) {
	primary, overrides := parseDBFlags([]string{})
	if primary != "" {
		t.Errorf("expected empty primary, got %q", primary)
	}
	if len(overrides) != 0 {
		t.Errorf("expected empty overrides, got %v", overrides)
	}
}

func TestParseDBFlags_SingleType(t *testing.T) {
	primary, overrides := parseDBFlags([]string{"postgres"})
	if primary != "postgres" {
		t.Errorf("expected 'postgres', got %q", primary)
	}
	if len(overrides) != 0 {
		t.Errorf("expected empty overrides, got %v", overrides)
	}
}

func TestParseDBFlags_NamedOverride(t *testing.T) {
	primary, overrides := parseDBFlags([]string{"primary=mysql", "secondary=postgres"})
	if primary != "" {
		t.Errorf("expected empty primary, got %q", primary)
	}
	if overrides["primary"] != "mysql" {
		t.Errorf("expected 'mysql' for primary, got %q", overrides["primary"])
	}
	if overrides["secondary"] != "postgres" {
		t.Errorf("expected 'postgres' for secondary, got %q", overrides["secondary"])
	}
}

func TestParseDBFlags_Mixed(t *testing.T) {
	primary, overrides := parseDBFlags([]string{"postgres", "analytics=mysql"})
	if primary != "postgres" {
		t.Errorf("expected 'postgres', got %q", primary)
	}
	if overrides["analytics"] != "mysql" {
		t.Errorf("expected 'mysql' for analytics, got %q", overrides["analytics"])
	}
}

func TestParseDBFlags_TableDriven(t *testing.T) {
	tests := []struct {
		name              string
		flags             []string
		expectedPrimary   string
		expectedOverrides map[string]string
	}{
		{
			name:              "empty",
			flags:             []string{},
			expectedPrimary:   "",
			expectedOverrides: map[string]string{},
		},
		{
			name:              "single type",
			flags:             []string{"postgres"},
			expectedPrimary:   "postgres",
			expectedOverrides: map[string]string{},
		},
		{
			name:              "named override",
			flags:             []string{"primary=mysql"},
			expectedPrimary:   "",
			expectedOverrides: map[string]string{"primary": "mysql"},
		},
		{
			name:              "mixed",
			flags:             []string{"postgres", "analytics=mysql"},
			expectedPrimary:   "postgres",
			expectedOverrides: map[string]string{"analytics": "mysql"},
		},
		{
			name:            "multiple overrides",
			flags:           []string{"db1=postgres", "db2=mysql"},
			expectedPrimary: "",
			expectedOverrides: map[string]string{
				"db1": "postgres",
				"db2": "mysql",
			},
		},
		{
			name:            "override with equals in value",
			flags:           []string{"db=type=foo"},
			expectedPrimary: "",
			expectedOverrides: map[string]string{
				"db": "type=foo",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			primary, overrides := parseDBFlags(tc.flags)
			if primary != tc.expectedPrimary {
				t.Errorf("primary = %q, want %q", primary, tc.expectedPrimary)
			}
			if len(overrides) != len(tc.expectedOverrides) {
				t.Errorf("overrides len = %d, want %d", len(overrides), len(tc.expectedOverrides))
			}
			for k, v := range tc.expectedOverrides {
				if overrides[k] != v {
					t.Errorf("overrides[%s] = %q, want %q", k, overrides[k], v)
				}
			}
		})
	}
}
