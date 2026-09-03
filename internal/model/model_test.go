package model

import "testing"

func TestParseStrategy(t *testing.T) {
	for _, value := range []string{"auto", "fixed", "prefer"} {
		if got, err := ParseStrategy(value); err != nil || string(got) != value {
			t.Fatalf("ParseStrategy(%q) = %q, %v", value, got, err)
		}
	}
	if _, err := ParseStrategy("fastest"); err == nil {
		t.Fatal("expected invalid strategy error")
	}
}

func TestChangeChanged(t *testing.T) {
	if (Change{Existed: true, Before: []byte("a"), After: []byte("a")}).Changed() {
		t.Fatal("equal content must not be changed")
	}
	if !(Change{Existed: true, Before: []byte("a"), After: []byte("b")}).Changed() {
		t.Fatal("different content must be changed")
	}
}
