package storage

import "testing"

func TestScrubCredentials(t *testing.T) {
	cases := map[string]string{
		"https://x-access-token:ghp_abc@github.com/o/r.git": "https://github.com/o/r.git",
		"https://ghp_abc@github.com/o/r.git":                "https://github.com/o/r.git",
		"https://github.com/o/r.git":                        "https://github.com/o/r.git",
		"git@github.com:o/r.git":                            "git@github.com:o/r.git", // não-https: inalterado
	}
	for in, want := range cases {
		if got := scrubCredentials(in); got != want {
			t.Errorf("scrubCredentials(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWebURLFromSource(t *testing.T) {
	cases := map[string]string{
		"https://github.com/o/r.git":            "https://github.com/o/r",
		"https://tok@github.com/o/r.git":         "https://github.com/o/r",
		"git@github.com:o/r.git":                 "https://github.com/o/r",
	}
	for in, want := range cases {
		if got := WebURLFromSource(in); got != want {
			t.Errorf("WebURLFromSource(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSafeBranchName(t *testing.T) {
	got := SafeBranchName("Feature: Nova Folder!")
	for _, r := range got {
		if r >= 'A' && r <= 'Z' {
			t.Errorf("SafeBranchName deixou maiúscula: %q", got)
		}
		if r == ' ' {
			t.Errorf("SafeBranchName deixou espaço: %q", got)
		}
	}
	if got == "" {
		t.Error("SafeBranchName devolveu vazio")
	}
}
