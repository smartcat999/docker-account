package cli

import (
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverDockerConfigReadsInlineDockerHubLogin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	auth := base64.StdEncoding.EncodeToString([]byte("alice:test-secret"))
	if err := os.WriteFile(path, []byte(`{"auths":{"https://index.docker.io/v1/":{"auth":"`+auth+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := func(_, _ string, _ []byte) ([]byte, error) {
		t.Fatal("credential helper should not be called")
		return nil, nil
	}

	items, err := discoverDockerConfig(path, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Username != "alice" || items[0].Registry != "docker.io" {
		t.Fatalf("unexpected discovery: %+v", items)
	}
	if items[0].Source != "Docker config" || items[0].Secret != "test-secret" {
		t.Fatalf("inline credential was not loaded correctly")
	}
}

func TestDiscoverDockerConfigUsesHelperListAndSkipsManagedCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"auths":{},"credsStore":"fake"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := func(helper, action string, input []byte) ([]byte, error) {
		if helper != "fake" {
			t.Fatalf("helper = %q", helper)
		}
		if action == "list" {
			return []byte(`{
				"https://index.docker.io/v1/":"alice",
				"registry.example.com":"bob",
				"https://index.docker.io/v1/.docker-account/alice":"alice"
			}`), nil
		}
		server := strings.TrimSpace(string(input))
		switch server {
		case "https://index.docker.io/v1/":
			return []byte(`{"Username":"<token>","Secret":"hub-token"}`), nil
		case "registry.example.com":
			return []byte(`{"Username":"bob","Secret":"registry-token"}`), nil
		default:
			t.Fatalf("unexpected helper lookup: %q", server)
			return nil, nil
		}
	}

	items, err := discoverDockerConfig(path, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("found %d accounts: %+v", len(items), items)
	}
	found := map[string]discoveredAccount{}
	for _, item := range items {
		found[item.Registry] = item
	}
	if found["docker.io"].Username != "alice" || found["registry.example.com"].Username != "bob" {
		t.Fatalf("unexpected accounts: %+v", found)
	}
	for _, item := range items {
		if item.Source != "docker-credential-fake" {
			t.Fatalf("source = %q", item.Source)
		}
	}
}

func TestDiscoverDockerConfigDeduplicatesDockerHubAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	auth := base64.StdEncoding.EncodeToString([]byte("alice:secret"))
	data := `{"auths":{"docker.io":{"auth":"` + auth + `"},"https://index.docker.io/v1/":{"auth":"` + auth + `"}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := discoverDockerConfig(path, func(_, _ string, _ []byte) ([]byte, error) {
		return nil, errors.New("not available")
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Registry != "docker.io" {
		t.Fatalf("unexpected discovery: %+v", items)
	}
}

func TestConfirmImportRepromptsAndDoesNotTreatEOFAsConsent(t *testing.T) {
	var out strings.Builder
	app := application{in: strings.NewReader("maybe\ny\n"), out: &out}
	confirmed, err := app.confirmImport()
	if err != nil || !confirmed {
		t.Fatalf("confirmed = %v, err = %v", confirmed, err)
	}
	if !strings.Contains(out.String(), "Please answer y or n.") {
		t.Fatalf("missing retry message: %q", out.String())
	}

	app = application{in: strings.NewReader(""), out: &out}
	if _, err := app.confirmImport(); !errors.Is(err, io.EOF) && (err == nil || !strings.Contains(err.Error(), "EOF")) {
		t.Fatalf("expected EOF without implicit consent, got %v", err)
	}
}

func TestImportedProfileNamesAreSafeAndUnique(t *testing.T) {
	if got := suggestProfileName("Alice Smith", "docker.io"); got != "Alice-Smith" {
		t.Fatalf("profile = %q", got)
	}
	used := map[string]bool{"alice": true, "alice-2": true}
	if got := uniqueProfileName("alice", used); got != "alice-3" {
		t.Fatalf("unique profile = %q", got)
	}
}
