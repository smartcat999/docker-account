package accounts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAccountLifecycle(t *testing.T) {
	store := New(t.TempDir())
	store.Now = func() time.Time { return time.Date(2026, 8, 3, 1, 2, 3, 0, time.UTC) }
	account, err := store.Add("work", "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if account.Registry != DefaultRegistry {
		t.Fatalf("registry = %q", account.Registry)
	}
	if err := store.Use("work"); err != nil {
		t.Fatal(err)
	}
	current, err := store.Current()
	if err != nil || current != "work" {
		t.Fatalf("current = %q, err = %v", current, err)
	}
	linkTarget, err := os.Readlink(store.CurrentLink())
	if err != nil {
		t.Fatal(err)
	}
	if linkTarget != filepath.Join(store.Root, "work") {
		t.Fatalf("current link = %q", linkTarget)
	}
	if err := store.Remove("work", false); err == nil {
		t.Fatal("expected current-account removal to fail")
	}
	if err := store.Remove("work", true); err != nil {
		t.Fatal(err)
	}
	current, err = store.Current()
	if err != nil || current != "" {
		t.Fatalf("current after remove = %q, err = %v", current, err)
	}
}

func TestRejectsUnsafeNames(t *testing.T) {
	store := New(t.TempDir())
	for _, name := range []string{"", "../work", "a/b", ".", "..", "current", "with space"} {
		if _, err := store.Add(name, "alice", ""); err == nil {
			t.Errorf("Add(%q) succeeded", name)
		}
	}
}

func TestHasCredentials(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.Add("work", "alice", ""); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(store.ConfigDir("work"), "config.json")
	if err := os.WriteFile(config, []byte(`{"auths":{"https://index.docker.io/v1/":{"auth":"x"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !store.HasCredentials("work") {
		t.Fatal("credentials not detected")
	}
}

func TestConfigureDockerCredentialStore(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.Add("work", "alice", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.ConfigureDockerCredentialStore("work", "docker-account"); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(t.TempDir(), "cli-plugins")
	if err := store.ConfigureCLIPluginDir("work", pluginDir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(store.ConfigDir("work"), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !strings.Contains(got, `"credsStore": "docker-account"`) {
		t.Fatalf("unexpected config: %s", got)
	}
	if got := string(data); !strings.Contains(got, `"cliPluginsExtraDirs"`) || !strings.Contains(got, pluginDir) {
		t.Fatalf("plugin directory missing from config: %s", got)
	}
}

func TestInheritDockerRuntimeConfigPreservesCredentials(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.Add("work", "alice", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.ConfigureDockerCredentialStore("work", "docker-account"); err != nil {
		t.Fatal(err)
	}
	dockerDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dockerDir, "config.json"), []byte(`{
		"auths":{"docker.io":{"auth":"system"}},
		"credsStore":"osxkeychain",
		"currentContext":"orbstack",
		"features":{"hooks":"true"}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.InheritDockerRuntimeConfig("work", dockerDir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(store.ConfigDir("work"), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, `"currentContext": "orbstack"`) || !strings.Contains(got, `"features"`) {
		t.Fatalf("runtime config not inherited: %s", got)
	}
	if !strings.Contains(got, `"credsStore": "docker-account"`) || strings.Contains(got, `"auth": "system"`) {
		t.Fatalf("account credentials overwritten: %s", got)
	}
}

func TestShareDockerRuntimeStatePreservesExistingData(t *testing.T) {
	store := New(t.TempDir())
	store.Now = func() time.Time { return time.Date(2026, 8, 3, 1, 2, 3, 0, time.UTC) }
	if _, err := store.Add("work", "alice", ""); err != nil {
		t.Fatal(err)
	}
	dockerDir := t.TempDir()
	for _, entry := range []string{"contexts", "buildx"} {
		if err := os.Mkdir(filepath.Join(dockerDir, entry), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	localBuildx := filepath.Join(store.ConfigDir("work"), "buildx")
	if err := os.Mkdir(localBuildx, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localBuildx, "state"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ShareDockerRuntimeState("work", dockerDir); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{"contexts", "buildx"} {
		resolved, err := filepath.EvalSymlinks(filepath.Join(store.ConfigDir("work"), entry))
		wanted, wantedErr := filepath.EvalSymlinks(filepath.Join(dockerDir, entry))
		if err != nil || wantedErr != nil || resolved != wanted {
			t.Fatalf("%s link = %q, err = %v", entry, resolved, err)
		}
	}
	backup := localBuildx + ".account-backup-20260803T010203Z"
	data, err := os.ReadFile(filepath.Join(backup, "state"))
	if err != nil || string(data) != "keep" {
		t.Fatalf("existing Buildx state was not preserved: %q, %v", data, err)
	}
}
