package core

import (
	"strings"
	"testing"
)

func TestRoleMatchEval(t *testing.T) {
	attrs := map[string]any{
		"role":     "the_admin_dude",
		"userid":   float64(7),
		"disabled": true,
		"missing":  nil,
		"account": map[string]any{
			"tier": "enterprise",
		},
	}

	tests := []struct {
		name  string
		match string
		want  bool
	}{
		{
			name:  "string equality",
			match: "role = 'the_admin_dude'",
			want:  true,
		},
		{
			name:  "number comparison",
			match: "userid < 10",
			want:  true,
		},
		{
			name:  "boolean logic precedence",
			match: "role = 'member' or userid < 10 and disabled = true",
			want:  true,
		},
		{
			name:  "parentheses and not",
			match: "not (role = 'member' or userid > 10)",
			want:  true,
		},
		{
			name:  "dotted path",
			match: "account.tier = 'enterprise'",
			want:  true,
		},
		{
			name:  "null check",
			match: "missing is null",
			want:  true,
		},
		{
			name:  "not null check",
			match: "account.tier is not null",
			want:  true,
		},
		{
			name:  "not equal",
			match: "role <> 'guest'",
			want:  true,
		},
		{
			name:  "false result",
			match: "role = 'member' and userid < 10",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parseRoleMatch(tt.match)
			if err != nil {
				t.Fatalf("parseRoleMatch() error = %v", err)
			}
			got, err := expr.eval(attrs)
			if err != nil {
				t.Fatalf("eval() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("eval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRoleMatchParseErrors(t *testing.T) {
	tests := []string{
		"userid <",
		"role = 'admin",
		"role between 1 and 2",
		"account..tier = 'enterprise'",
		"(role = 'admin'",
	}

	for _, match := range tests {
		t.Run(match, func(t *testing.T) {
			_, err := parseRoleMatch(match)
			if err == nil {
				t.Fatalf("parseRoleMatch(%q) expected error", match)
			}
			if strings.TrimSpace(err.Error()) == "" {
				t.Fatalf("parseRoleMatch(%q) returned empty error", match)
			}
		})
	}
}
