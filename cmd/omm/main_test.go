package main

import (
	"testing"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

func TestSplitList(t *testing.T) {
	got := splitList(" pip, npm ,,cargo ")
	if len(got) != 3 || got[0] != "pip" || got[2] != "cargo" {
		t.Fatalf("unexpected list: %#v", got)
	}
}

func TestSelectionURLsAreStable(t *testing.T) {
	selection := model.Selection{Endpoint: "https://one.example", Endpoints: map[string]string{"z": "https://z.example", "a": "https://a.example"}}
	got := selectionURLs(selection)
	if len(got) != 3 || got[1] != "https://a.example" || got[2] != "https://z.example" {
		t.Fatalf("unexpected URLs: %#v", got)
	}
}

func TestBenchmarkURLsUseProtocolEntryPoints(t *testing.T) {
	pypi := benchmarkURLs("pypi", model.Selection{Endpoint: "https://mirror.example/simple/"})
	if len(pypi) != 1 || pypi[0] != "https://mirror.example/simple/pip/" {
		t.Fatalf("unexpected PyPI probe: %#v", pypi)
	}
	homebrew := benchmarkURLs("homebrew", model.Selection{Endpoints: map[string]string{"api": "https://mirror.example/api", "bottles": "https://mirror.example/bottles"}})
	if len(homebrew) != 1 || homebrew[0] != "https://mirror.example/api/formula.jws.json" {
		t.Fatalf("unexpected Homebrew probe: %#v", homebrew)
	}
}

func TestNormalizeAdapterAliases(t *testing.T) {
	got := normalizeAdapterList([]string{"pip", "uv", "brew", "npm"})
	want := []string{"pypi", "homebrew", "npm"}
	if len(got) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v", got)
		}
	}
}
