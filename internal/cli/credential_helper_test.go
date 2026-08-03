package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pengwu/docker-account/internal/accounts"
)

func TestNamespaceCredentialStorePayload(t *testing.T) {
	payload := []byte(`{"ServerURL":"https://index.docker.io/v1/","Username":"alice","Secret":"token"}`)
	result, err := namespaceCredentialPayload("store", payload, "company account")
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(result, &value); err != nil {
		t.Fatal(err)
	}
	if got, want := value["ServerURL"], "https://index.docker.io/v1/.docker-account/company%20account"; got != want {
		t.Fatalf("ServerURL = %q, want %q", got, want)
	}
	if value["Secret"] != "token" {
		t.Fatal("secret changed while namespacing payload")
	}
}

func TestNamespaceCredentialGetAndErasePayload(t *testing.T) {
	for _, operation := range []string{"get", "erase"} {
		result, err := namespaceCredentialPayload(operation, []byte("docker.io\n"), "work")
		if err != nil {
			t.Fatal(err)
		}
		if got := string(result); got != "docker.io/.docker-account/work" {
			t.Fatalf("%s payload = %q", operation, got)
		}
	}
}

func TestNamespaceCredentialPayloadRejectsUnknownOperation(t *testing.T) {
	_, err := namespaceCredentialPayload("list", nil, "work")
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCredentialHelperDelegatesWithAccountNamespace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}
	root := t.TempDir()
	store := accounts.New(root)
	if _, err := store.Add("work", "alice", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetCredentialStore("work", "fake"); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	helper := filepath.Join(binDir, "docker-credential-fake")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_ACCOUNT_HOME", root)
	t.Setenv("DOCKER_ACCOUNT_NAME", "work")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var out, errOut bytes.Buffer
	code := RunCredentialHelper([]string{"get"}, strings.NewReader("docker.io\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, errOut.String())
	}
	if got := out.String(); got != "docker.io/.docker-account/work" {
		t.Fatalf("delegated payload = %q", got)
	}
}

func TestNamespaceServerURLUsesKeychainPath(t *testing.T) {
	tests := map[string]string{
		"https://index.docker.io/v1/": "https://index.docker.io/v1/.docker-account/default",
		"docker.io":                   "docker.io/.docker-account/default",
	}
	for input, want := range tests {
		got, err := namespaceServerURL(input, "default")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("namespaceServerURL(%q) = %q, want %q", input, got, want)
		}
	}
}
