package safeurl

import (
	"context"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, good := range []string{"https://example.com/repo", "sparse+https://example.com/index/"} {
		if err := Validate(good, false); err != nil {
			t.Fatalf("Validate(%q): %v", good, err)
		}
	}
	for _, bad := range []string{"http://example.com", "https://user:secret@example.com", "https://127.0.0.1/repo", "https://localhost/repo", "https://example.com/repo?token=x", "https://example.com/repo#fragment"} {
		if err := Validate(bad, false); err == nil {
			t.Fatalf("Validate(%q) unexpectedly succeeded", bad)
		}
	}
	if err := Validate("https://127.0.0.1/repo", true); err != nil {
		t.Fatalf("private opt-in failed: %v", err)
	}
}

func TestDialContextRejectsResolvedLoopback(t *testing.T) {
	_, err := DialContext(false)(context.Background(), "tcp", "localhost:443")
	if err == nil {
		t.Fatal("expected loopback resolution to be rejected")
	}
}

func FuzzValidate(f *testing.F) {
	for _, seed := range []string{"https://example.com/", "sparse+https://example.com/index/", "https://127.0.0.1", "://"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		_ = Validate(value, false)
		_ = Redact(value)
	})
}

func TestRedact(t *testing.T) {
	got := Redact("https://alice:secret@example.com/repo?token=abc&x=1")
	if got != "https://REDACTED@example.com/repo?token=REDACTED&x=1" {
		t.Fatalf("unexpected redaction: %s", got)
	}
}
