package apt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

func ubuntuEnv(t *testing.T) (model.Environment, string) {
	t.Helper()
	root := t.TempDir()
	aptDir := filepath.Join(root, "etc", "apt")
	if err := os.MkdirAll(filepath.Join(aptDir, "sources.list.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "os-release"), []byte("ID=ubuntu\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return model.Environment{SystemRoot: root, IncludeSystem: true}, aptDir
}

func TestPlanPreservesSecurityAndUsesPortsEndpoint(t *testing.T) {
	env, dir := ubuntuEnv(t)
	p := filepath.Join(dir, "sources.list")
	before := "deb [arch=amd64] http://archive.ubuntu.com/ubuntu noble main universe\ndeb-src http://archive.ubuntu.com/ubuntu noble main\ndeb http://security.ubuntu.com/ubuntu noble-security main\ndeb http://ports.ubuntu.com/ubuntu-ports noble main\ndeb https://packages.example.test/vendor stable main\ndeb https://download.docker.com/linux/ubuntu noble stable\n"
	if err := os.WriteFile(p, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	selection := model.Selection{Endpoints: map[string]string{"apt": "mirror+https://mirror.example/ubuntu", "apt-ports": "https://ports-mirror.example/ubuntu-ports"}}
	changes, err := New().Plan(context.Background(), env, selection)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("got %d changes", len(changes))
	}
	got := string(changes[0].After)
	for _, want := range []string{"deb [arch=amd64] mirror+https://mirror.example/ubuntu noble", "deb-src mirror+https://mirror.example/ubuntu noble", "http://security.ubuntu.com/ubuntu noble-security", "https://ports-mirror.example/ubuntu-ports noble"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "deb https://packages.example.test/vendor stable main") {
		t.Fatalf("third-party repository was modified:\n%s", got)
	}
	if !strings.Contains(got, "deb https://download.docker.com/linux/ubuntu noble stable") {
		t.Fatalf("Docker repository was modified:\n%s", got)
	}
	if err := os.WriteFile(p, changes[0].After, 0o644); err != nil {
		t.Fatal(err)
	}
	if verified := New().Verify(context.Background(), env, selection); !verified.OK {
		t.Fatal(verified.Detail)
	}
}

func TestDEB822PlanAndSecurityOption(t *testing.T) {
	env, dir := ubuntuEnv(t)
	p := filepath.Join(dir, "sources.list.d", "ubuntu.sources")
	before := "Types: deb deb-src\nURIs: http://archive.ubuntu.com/ubuntu\nSuites: noble noble-updates\nComponents: main restricted\n\nTypes: deb\nURIs: http://security.ubuntu.com/ubuntu\nSuites: noble-security\nComponents: main\n\nTypes: deb\nURIs: https://packages.example.test/vendor\nSuites: stable\nComponents: main\n"
	if err := os.WriteFile(p, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	selection := model.Selection{Endpoint: "https://mirror.example/ubuntu"}
	changes, err := New().Plan(context.Background(), env, selection)
	if err != nil {
		t.Fatal(err)
	}
	got := string(changes[0].After)
	if !strings.Contains(got, "URIs: https://mirror.example/ubuntu") || !strings.Contains(got, "URIs: http://security.ubuntu.com/ubuntu") {
		t.Fatalf("security preservation failed:\n%s", got)
	}
	env.IncludeSecurity = true
	changes, err = New().Plan(context.Background(), env, selection)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(changes[0].After), "URIs: https://mirror.example/ubuntu") != 2 {
		t.Fatalf("security source was not switched:\n%s", changes[0].After)
	}
	if !strings.Contains(string(changes[0].After), "URIs: https://packages.example.test/vendor") {
		t.Fatal("third-party DEB822 repository was modified")
	}
}

func TestPlanSkipsSystemSourcesWithoutExplicitSystemFlag(t *testing.T) {
	env, dir := ubuntuEnv(t)
	env.IncludeSystem = false
	if err := os.WriteFile(filepath.Join(dir, "sources.list"), []byte("deb https://archive.ubuntu.com/ubuntu noble main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := New().Plan(context.Background(), env, model.Selection{Endpoint: "https://mirror.example/ubuntu"})
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes=%d err=%v", len(changes), err)
	}
}

func TestAutoSelectionUsesMirrorTransport(t *testing.T) {
	got, err := aptEndpoint(model.Selection{Strategy: model.StrategyAuto, Endpoint: "https://mirrors.cernet.edu.cn/ubuntu"}, "ubuntu", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "mirror+https://mirrors.cernet.edu.cn/ubuntu" {
		t.Fatalf("got %q", got)
	}
}

func TestAutoSelectionUsesDistroSpecificEndpoints(t *testing.T) {
	selection := model.Selection{Strategy: model.StrategyAuto, Endpoints: map[string]string{
		"ubuntu":          "https://mirrors.cernet.edu.cn/api/apt/mirrorlist/ubuntu",
		"ubuntu-ports":    "https://mirrors.cernet.edu.cn/api/apt/mirrorlist/ubuntu-ports",
		"debian":          "https://mirrors.cernet.edu.cn/api/apt/mirrorlist/debian",
		"debian-security": "https://mirrors.cernet.edu.cn/api/apt/mirrorlist/debian-security",
	}}
	ubuntu, err := aptEndpoint(selection, "ubuntu", false)
	if err != nil || !strings.HasSuffix(ubuntu, "/ubuntu") {
		t.Fatalf("ubuntu=%q err=%v", ubuntu, err)
	}
	ports, err := aptEndpoint(selection, "ubuntu", true)
	if err != nil || !strings.HasSuffix(ports, "/ubuntu-ports") {
		t.Fatalf("ports=%q err=%v", ports, err)
	}
	debian, err := aptEndpoint(selection, "debian", false)
	if err != nil || !strings.HasSuffix(debian, "/debian") {
		t.Fatalf("debian=%q err=%v", debian, err)
	}
	security, err := aptSecurityEndpoint(selection, "debian")
	if err != nil || !strings.HasSuffix(security, "/debian-security") {
		t.Fatalf("security=%q err=%v", security, err)
	}
}

func TestDebianSecurityUsesDedicatedEndpointWhenExplicitlyIncluded(t *testing.T) {
	root := t.TempDir()
	aptDir := filepath.Join(root, "etc", "apt")
	if err := os.MkdirAll(filepath.Join(aptDir, "sources.list.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "os-release"), []byte("ID=debian\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(aptDir, "sources.list")
	before := "deb https://deb.debian.org/debian trixie main\ndeb https://security.debian.org/debian-security trixie-security main\n"
	if err := os.WriteFile(p, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	env := model.Environment{SystemRoot: root, IncludeSystem: true, IncludeSecurity: true}
	selection := model.Selection{Endpoints: map[string]string{
		"debian":          "https://mirror.example/debian",
		"debian-security": "https://mirror.example/debian-security",
	}}
	changes, err := New().Plan(context.Background(), env, selection)
	if err != nil {
		t.Fatal(err)
	}
	got := string(changes[0].After)
	if !strings.Contains(got, "https://mirror.example/debian trixie main") || !strings.Contains(got, "https://mirror.example/debian-security trixie-security main") {
		t.Fatalf("dedicated security endpoint not used:\n%s", got)
	}
}
