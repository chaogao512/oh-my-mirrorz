package main

import "testing"

func TestSplitList(t *testing.T) {
	got := splitList(" pip, npm ,,cargo ")
	if len(got) != 3 || got[0] != "pip" || got[2] != "cargo" {
		t.Fatalf("unexpected list: %#v", got)
	}
}

func TestFinalTargetShowsRedirectHost(t *testing.T) {
	if got := finalTarget("https://mirrors.ustc.edu.cn/pypi/web/simple/pip/"); got != "mirrors.ustc.edu.cn" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeAdapterAliases(t *testing.T) {
	got := normalizeAdapterList([]string{"pip", "uv", "brew", "npm", "mamba", "micromamba"})
	want := []string{"pypi", "homebrew", "npm", "conda"}
	if len(got) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v", got)
		}
	}
}
