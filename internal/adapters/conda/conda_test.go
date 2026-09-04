package conda

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

func testEnv(t *testing.T) model.Environment {
	t.Helper()
	return model.Environment{Home: t.TempDir(), GOOS: "darwin", GOARCH: "arm64"}
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func selection(endpoint string) model.Selection {
	return model.Selection{AdapterID: adapterID, Mirror: "auto", Endpoint: endpoint, Strategy: model.StrategyAuto}
}

func TestPlanPreservesChannelPolicyAndUnrelatedSettings(t *testing.T) {
	env := testEnv(t)
	before := "# keep this comment\nchannels:\n  - defaults\n  - conda-forge\n  - private-lab\nchannel_priority: strict\nssl_verify: true\ncustom_channels:\n  private-lab: https://packages.example.edu/conda\n"
	writeConfig(t, configPath(env), before)
	changes, err := New().Plan(context.Background(), env, selection("https://mirrors.example.edu/anaconda"))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes=%#v", changes)
	}
	after := string(changes[0].After)
	for _, want := range []string{
		"# keep this comment", "- defaults", "- conda-forge", "- private-lab",
		"channel_priority: strict", "ssl_verify: true",
		"private-lab: https://packages.example.edu/conda",
		"conda-forge: https://mirrors.example.edu/anaconda/cloud",
		"- https://mirrors.example.edu/anaconda/pkgs/main",
		"- https://mirrors.example.edu/anaconda/pkgs/r",
	} {
		if !strings.Contains(after, want) {
			t.Fatalf("missing %q:\n%s", want, after)
		}
	}
	writeConfig(t, configPath(env), after)
	again, err := New().Plan(context.Background(), env, selection("https://mirrors.example.edu/anaconda"))
	if err != nil || len(again) != 1 || again[0].Changed() {
		t.Fatalf("second plan must be idempotent: changes=%#v err=%v", again, err)
	}
}

func TestCommunityOnlyDoesNotAddDefaults(t *testing.T) {
	env := testEnv(t)
	writeConfig(t, configPath(env), "channels:\n  - conda-forge\n  - nodefaults\ndefault_channels:\n  - https://inactive.example/pkgs/main\n")
	changes, err := New().Plan(context.Background(), env, selection("https://mirror.example/anaconda"))
	if err != nil {
		t.Fatal(err)
	}
	after := string(changes[0].After)
	if !strings.Contains(after, "https://inactive.example/pkgs/main") || !strings.Contains(after, "conda-forge: https://mirror.example/anaconda/cloud") {
		t.Fatalf("unexpected config:\n%s", after)
	}
}

func TestEmptyConfigPreservesEffectiveDefaults(t *testing.T) {
	env := testEnv(t)
	writeConfig(t, configPath(env), "")
	changes, err := New().Plan(context.Background(), env, selection("https://mirror.example/anaconda"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(changes[0].After), "default_channels:") {
		t.Fatalf("defaults were not mirrored:\n%s", changes[0].After)
	}
}

func TestPlanRefusesCredentialBearingPublicChannel(t *testing.T) {
	env := testEnv(t)
	writeConfig(t, configPath(env), "channels:\n  - conda-forge\ncustom_channels:\n  conda-forge: https://token@example.com/cloud\n")
	_, err := New().Plan(context.Background(), env, selection("https://mirror.example/anaconda"))
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("expected credential conflict, got %v", err)
	}
}

func TestDetectRejectsDuplicateKeys(t *testing.T) {
	env := testEnv(t)
	writeConfig(t, configPath(env), "channels:\n  - defaults\nchannels:\n  - conda-forge\n")
	detection := New().Detect(context.Background(), env)
	if detection.Status != model.StatusInvalidConfig || !strings.Contains(detection.Reason, "duplicate key") {
		t.Fatalf("unexpected detection: %#v", detection)
	}
}

func TestPlanLeavesPrivateOnlyConfigurationByteForByteUntouched(t *testing.T) {
	env := testEnv(t)
	before := "# private only\nchannels: [private-lab]\ncustom_channels:\n  private-lab: https://packages.example.edu/conda\n"
	writeConfig(t, configPath(env), before)
	changes, err := New().Plan(context.Background(), env, selection("https://mirror.example/anaconda"))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("private-only config should not be rewritten: %#v", changes)
	}
}

func TestDetectRejectsShadowChannelConfiguration(t *testing.T) {
	env := testEnv(t)
	writeConfig(t, filepath.Join(env.Home, ".mambarc"), "channels:\n  - conda-forge\n")
	detection := New().Detect(context.Background(), env)
	if detection.Status != model.StatusInvalidConfig || !strings.Contains(detection.Reason, "also controls channels") {
		t.Fatalf("unexpected detection: %#v", detection)
	}
}

func TestProbeTargetsFollowPolicyAndPlatform(t *testing.T) {
	env := testEnv(t)
	writeConfig(t, configPath(env), "channels:\n  - defaults\n  - conda-forge\n")
	targets, err := New().ProbeTargets(env, selection("https://mirror.example/anaconda"))
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets=%#v", targets)
	}
	if targets[0].Capability != "defaults" || !strings.HasSuffix(targets[0].URL, "/pkgs/main/osx-arm64/repodata.json") {
		t.Fatalf("unexpected defaults target: %#v", targets[0])
	}
	if targets[1].Capability != "conda-forge" || !strings.HasSuffix(targets[1].URL, "/cloud/conda-forge/osx-arm64/repodata.json") {
		t.Fatalf("unexpected community target: %#v", targets[1])
	}
}

func TestPlatformSubdir(t *testing.T) {
	tests := map[string]string{"darwin/arm64": "osx-arm64", "darwin/amd64": "osx-64", "linux/amd64": "linux-64", "linux/arm64": "linux-aarch64", "freebsd/amd64": "noarch"}
	for input, want := range tests {
		parts := strings.Split(input, "/")
		if got := platformSubdir(parts[0], parts[1]); got != want {
			t.Fatalf("%s: got %s want %s", input, got, want)
		}
	}
}
