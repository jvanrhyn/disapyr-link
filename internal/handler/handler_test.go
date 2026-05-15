package handler

import (
	"testing"
)

func TestIsProbeRequest(t *testing.T) {
	tests := []struct {
		path   string
		expect bool
		desc   string
	}{
		// Git paths (should be blocked)
		{"/.git/config", true, "git config"},
		{"/.git/head", true, "git HEAD"},
		{"/.gitignore", true, "gitignore"},
		{"/.github/workflows", true, "github workflows"},

		// Site discovery (should be blocked)
		{"/robots.txt", true, "robots.txt"},
		{"/sitemap.xml", true, "sitemap.xml"},
		{"/favicon.ico", true, "favicon.ico"},
		{"/.well-known/acme-challenge/test", true, "well-known"},

		// WordPress/plugin paths (should be blocked)
		{"/wp-json/gravitysmtp/v1/tests/mock-data", true, "wordpress gravitysmtp"},
		{"/wp-admin", true, "wp-admin"},
		{"/wp-content/plugins", true, "wp-content"},
		{"/xmlrpc.php", true, "xmlrpc.php"},

		// Environment/config files (should be blocked)
		{"/.env", true, ".env"},
		{"/.env.local", true, ".env.local"},
		{"/config.php", true, "config.php"},
		{"/web.config", true, "web.config"},

		// Other common probes (should be blocked)
		{"/admin", true, "admin"},
		{"/backup", true, "backup"},
		{"/uploads", true, "uploads"},
		{"/node_modules", true, "node_modules"},

		// Legitimate paths (should NOT be blocked)
		{"/", false, "root index"},
		{"/s/token123", false, "secret token"},
		{"/static/css/style.css", false, "static css"},
		{"/static/js/crypto.js", false, "static js"},
		{"/health", false, "health endpoint (handled by auth)"},

		// Case insensitivity tests
		{"/.GIT/CONFIG", true, "uppercase git config"},
		{"/ROBOTS.TXT", true, "uppercase robots.txt"},
		{"/WP-JSON/test", true, "mixed case wp-json"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := isProbeRequest(tt.path)
			if got != tt.expect {
				t.Errorf("isProbeRequest(%q) = %v, want %v", tt.path, got, tt.expect)
			}
		})
	}
}
