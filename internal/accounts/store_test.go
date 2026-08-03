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
	data, err := os.ReadFile(filepath.Join(store.ConfigDir("work"), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !strings.Contains(got, `"credsStore": "docker-account"`) {
		t.Fatalf("unexpected config: %s", got)
	}
}
