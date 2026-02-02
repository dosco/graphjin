package main

import "testing"

func TestNormalizeMCPServerURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "IP address only",
			input:    "10.0.0.5",
			expected: "http://10.0.0.5/api/v1/mcp/message",
		},
		{
			name:     "IP with port",
			input:    "10.0.0.5:8080",
			expected: "http://10.0.0.5:8080/api/v1/mcp/message",
		},
		{
			name:     "hostname only",
			input:    "localhost",
			expected: "http://localhost/api/v1/mcp/message",
		},
		{
			name:     "hostname with port",
			input:    "localhost:8080",
			expected: "http://localhost:8080/api/v1/mcp/message",
		},
		{
			name:     "full HTTP URL without path",
			input:    "http://example.com",
			expected: "http://example.com/api/v1/mcp/message",
		},
		{
			name:     "full HTTP URL with just slash",
			input:    "http://example.com/",
			expected: "http://example.com/api/v1/mcp/message",
		},
		{
			name:     "full HTTP URL with custom path",
			input:    "http://example.com/custom/mcp",
			expected: "http://example.com/custom/mcp",
		},
		{
			name:     "HTTPS URL",
			input:    "https://secure.example.com",
			expected: "https://secure.example.com/api/v1/mcp/message",
		},
		{
			name:     "HTTPS URL with path",
			input:    "https://secure.example.com/api/v1/mcp/message",
			expected: "https://secure.example.com/api/v1/mcp/message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeMCPServerURL(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeMCPServerURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
