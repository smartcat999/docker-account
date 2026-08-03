package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/smartcat999/docker-account/internal/accounts"
)

// RunCredentialHelper implements Docker's credential-helper protocol and
// delegates storage to the platform helper after namespacing the registry key
// with the selected account. This allows multiple accounts for one registry to
// coexist in a native keychain.
func RunCredentialHelper(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(errOut, "credential helper expects one of: store, get, erase")
		return 1
	}
	name := strings.TrimSpace(os.Getenv("DOCKER_ACCOUNT_NAME"))
	if name == "" {
		root, err := accounts.DefaultRoot()
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		name, err = accounts.New(root).Current()
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		if name == "" {
			fmt.Fprintln(errOut, "no current Docker account is selected")
			return 1
		}
	}
	root, err := accounts.DefaultRoot()
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	account, err := accounts.New(root).Get(name)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if account.CredentialStore == "" || account.CredentialStore == "docker-account" {
		fmt.Fprintln(errOut, "account has no native credential store configured")
		return 1
	}
	payload, err := io.ReadAll(in)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	payload, err = namespaceCredentialPayload(args[0], payload, name)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	command := exec.Command("docker-credential-"+account.CredentialStore, args[0])
	command.Stdin = bytes.NewReader(payload)
	command.Stdout = out
	command.Stderr = errOut
	if err := command.Run(); err != nil {
		return exitCode(err)
	}
	return 0
}

func namespaceCredentialPayload(operation string, payload []byte, account string) ([]byte, error) {
	switch operation {
	case "store":
		var value map[string]any
		if err := json.Unmarshal(payload, &value); err != nil {
			return nil, fmt.Errorf("decode credential payload: %w", err)
		}
		server, ok := value["ServerURL"].(string)
		if !ok || server == "" {
			return nil, errors.New("credential payload has no ServerURL")
		}
		namespaced, err := namespaceServerURL(server, account)
		if err != nil {
			return nil, err
		}
		value["ServerURL"] = namespaced
		return json.Marshal(value)
	case "get", "erase":
		server := strings.TrimSpace(string(payload))
		if server == "" {
			return nil, errors.New("credential payload has no server URL")
		}
		namespaced, err := namespaceServerURL(server, account)
		if err != nil {
			return nil, err
		}
		return []byte(namespaced), nil
	default:
		return nil, fmt.Errorf("unsupported credential helper operation %q", operation)
	}
}

func namespaceServerURL(server, account string) (string, error) {
	// Account names are validated to filesystem-safe URL path characters.
	segment := ".docker-account/" + account
	if !strings.Contains(server, "://") {
		return strings.TrimRight(server, "/") + "/" + segment, nil
	}
	parsed, err := url.Parse(server)
	if err != nil {
		return "", fmt.Errorf("parse credential server URL: %w", err)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("credential server URL %q has no host", server)
	}
	parsed.Path = path.Join(parsed.Path, segment)
	parsed.RawPath = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}
