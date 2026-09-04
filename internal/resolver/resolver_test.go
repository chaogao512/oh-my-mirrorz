package resolver

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestResolveAutoPerAdapter(t *testing.T) {
	r := New()
	r.Clock = func() time.Time { return time.Unix(1, 0) }
	got, err := r.Resolve("pypi", model.StrategyAuto, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Mirror != "auto" || got.Endpoint == "" || got.ResolvedAt.Unix() != 1 {
		t.Fatalf("unexpected selection: %#v", got)
	}
}

func TestResolvePreferFallsBack(t *testing.T) {
	r := New()
	got, err := r.Resolve("npm", model.StrategyPrefer, "tuna")
	if err != nil {
		t.Fatal(err)
	}
	if got.Mirror != "auto" {
		t.Fatalf("expected auto fallback: %#v", got)
	}
}

func TestResolveFixedDoesNotGuess(t *testing.T) {
	r := New()
	if _, err := r.Resolve("npm", model.StrategyFixed, "tuna"); err == nil {
		t.Fatal("expected unsupported fixed mirror to fail")
	}
}

func TestMirrorsAreStableAndUnique(t *testing.T) {
	got := BuiltInCatalog().Mirrors("pypi")
	want := []string{"auto", "tuna", "ustc"}
	if len(got) != len(want) {
		t.Fatalf("unexpected mirrors: %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected mirrors: %#v", got)
		}
	}
}

func TestCondaCandidatesShareTheCatalog(t *testing.T) {
	candidates, err := New().Candidates("conda")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 || candidates[0].Mirror != "auto" || candidates[1].Mirror != "tuna" || candidates[2].Mirror != "ustc" {
		t.Fatalf("unexpected candidates: %#v", candidates)
	}
}

func TestHTTPProberUsesBoundedGetFallback(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("User-Agent") != "oh-my-mirrorz/0.2" {
			t.Errorf("unexpected user agent %q", r.Header.Get("User-Agent"))
		}
		if r.Method == http.MethodHead {
			return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader("forbidden")), Request: r}, nil
		}
		if r.Header.Get("Range") != "bytes=0-0" {
			t.Error("GET fallback is not range-bounded")
		}
		return &http.Response{StatusCode: http.StatusPartialContent, Body: io.NopCloser(strings.NewReader("x")), Request: r}, nil
	})}
	result, err := (HTTPProber{Client: client}).Probe(context.Background(), "https://example.com/resource")
	if err != nil || result.Status != http.StatusPartialContent {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
