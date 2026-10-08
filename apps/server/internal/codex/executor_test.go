package codex

import (
	"slices"
	"testing"
)

func TestCodexProcessEnvironmentUsesAllowlist(t *testing.T) {
	source := []string{
		"PATH=/usr/bin",
		"HOME=/tmp/home",
		"LANG=zh_CN.UTF-8",
		"https_proxy=http://proxy.example",
		"AWS_SECRET_ACCESS_KEY=must-not-leak",
		"GITHUB_TOKEN=must-not-leak",
		"DATABASE_URL=must-not-leak",
		"NODE_ENV=development",
		"invalid-entry-without-equals",
	}

	got := codexProcessEnvironment(source)
	want := []string{
		"PATH=/usr/bin",
		"HOME=/tmp/home",
		"LANG=zh_CN.UTF-8",
		"https_proxy=http://proxy.example",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("codexProcessEnvironment() = %#v, want %#v", got, want)
	}
}
