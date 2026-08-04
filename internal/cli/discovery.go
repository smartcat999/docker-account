package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/smartcat999/docker-account/internal/accounts"
)

type discoveredAccount struct {
	Profile   string
	Username  string
	Registry  string
	ServerURL string
	Source    string
	Secret    string
}

type importResult struct {
	Found    int
	Imported int
	Declined bool
}

type credentialCommand func(helper, action string, input []byte) ([]byte, error)

type dockerCredentialConfig struct {
	Auths map[string]struct {
		Auth          string `json:"auth"`
		IdentityToken string `json:"identitytoken"`
	} `json:"auths"`
	CredentialsStore  string            `json:"credsStore"`
	CredentialHelpers map[string]string `json:"credHelpers"`
}

func (a *application) importAccounts(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: docker account import")
	}
	_, err := a.importSystemAccounts()
	return err
}

func (a *application) importSystemAccounts() (importResult, error) {
	discovered, err := a.discoverSystemAccounts()
	if err != nil {
		return importResult{}, err
	}
	result := importResult{Found: len(discovered)}
	if len(discovered) == 0 {
		fmt.Fprintln(a.out, "No existing Docker logins found.")
		fmt.Fprintln(a.out, "Add one with: docker account add NAME --username USER --login")
		return result, nil
	}

	fmt.Fprintln(a.out, "Existing Docker logins found:")
	w := tabwriter.NewWriter(a.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PROFILE\tUSERNAME\tREGISTRY\tSOURCE")
	for _, item := range discovered {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", item.Profile, item.Username, item.Registry, item.Source)
	}
	if err := w.Flush(); err != nil {
		return result, err
	}

	confirmed, err := a.confirmImport()
	if err != nil {
		return result, err
	}
	if !confirmed {
		result.Declined = true
		fmt.Fprintln(a.out, "No accounts were imported.")
		return result, nil
	}

	for _, item := range discovered {
		account, err := a.store.Add(item.Profile, item.Username, item.Registry)
		if err != nil {
			return result, fmt.Errorf("import %s: %w", item.Profile, err)
		}
		if err := a.ensurePlugin(item.Profile); err != nil {
			_ = a.store.Remove(item.Profile, true)
			return result, fmt.Errorf("prepare imported account %s: %w", item.Profile, err)
		}
		if err := a.saveDiscoveredCredential(&account, item); err != nil {
			_ = a.store.Remove(item.Profile, true)
			return result, fmt.Errorf("save imported credentials for %s: %w", item.Profile, err)
		}
		if _, err := a.store.ActivateIfUnset(item.Profile); err != nil {
			return result, err
		}
		result.Imported++
		fmt.Fprintf(a.out, "Imported %s (%s@%s).\n", item.Profile, item.Username, item.Registry)
	}
	return result, nil
}

func (a *application) confirmImport() (bool, error) {
	for {
		fmt.Fprint(a.out, "Import and manage these accounts? [Y/n] ")
		value, err := readLine(a.in)
		if err != nil {
			return false, fmt.Errorf("read import confirmation: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(a.out, "Please answer y or n.")
		}
	}
}

func readLine(input io.Reader) (string, error) {
	var value strings.Builder
	var buffer [1]byte
	for {
		n, err := input.Read(buffer[:])
		if n > 0 {
			if buffer[0] == '\n' {
				return value.String(), nil
			}
			if buffer[0] != '\r' {
				value.WriteByte(buffer[0])
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if value.Len() == 0 {
					return "", io.EOF
				}
				return value.String(), nil
			}
			return "", err
		}
	}
}

func (a *application) discoverSystemAccounts() ([]discoveredAccount, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve Docker configuration: %w", err)
	}
	defaultDir := filepath.Join(home, ".docker")
	dirs := []string{defaultDir}
	if configured := strings.TrimSpace(os.Getenv("DOCKER_CONFIG")); configured != "" {
		configured, err = filepath.Abs(configured)
		if err == nil && filepath.Clean(configured) != filepath.Clean(defaultDir) && !pathWithin(configured, a.store.Root) {
			dirs = append([]string{configured}, dirs...)
		}
	}

	existing, err := a.store.List()
	if err != nil {
		return nil, err
	}
	existingKeys := map[string]bool{}
	usedProfiles := map[string]bool{}
	for _, item := range existing {
		existingKeys[discoveryKey(item.Username, item.Registry)] = true
		usedProfiles[item.Name] = true
	}

	seenConfig := map[string]bool{}
	seenAccount := map[string]bool{}
	var result []discoveredAccount
	for _, dir := range dirs {
		path := filepath.Join(dir, "config.json")
		resolved, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr == nil {
			path = resolved
		}
		if seenConfig[path] {
			continue
		}
		seenConfig[path] = true
		items, err := discoverDockerConfig(path, a.runCredentialCommand)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			key := discoveryKey(item.Username, item.Registry)
			if existingKeys[key] || seenAccount[key] {
				continue
			}
			item.Profile = uniqueProfileName(suggestProfileName(item.Username, item.Registry), usedProfiles)
			usedProfiles[item.Profile] = true
			seenAccount[key] = true
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Registry == result[j].Registry {
			return result[i].Username < result[j].Username
		}
		return result[i].Registry < result[j].Registry
	})
	return result, nil
}

func discoverDockerConfig(path string, runner credentialCommand) ([]discoveredAccount, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config dockerCredentialConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if config.Auths == nil {
		config.Auths = map[string]struct {
			Auth          string `json:"auth"`
			IdentityToken string `json:"identitytoken"`
		}{}
	}

	type candidate struct {
		server       string
		usernameHint string
	}
	candidates := map[string]candidate{}
	for server := range config.Auths {
		if !isManagedCredentialServer(server) {
			candidates[server] = candidate{server: server}
		}
	}
	for server := range config.CredentialHelpers {
		if !isManagedCredentialServer(server) {
			candidates[server] = candidate{server: server}
		}
	}
	if helper := usableCredentialHelper(config.CredentialsStore); helper != "" {
		if output, listErr := runner(helper, "list", nil); listErr == nil {
			var listed map[string]string
			if json.Unmarshal(output, &listed) == nil {
				for server, username := range listed {
					if !isManagedCredentialServer(server) {
						candidates[server] = candidate{server: server, usernameHint: username}
					}
				}
			}
		}
	}

	seen := map[string]bool{}
	var result []discoveredAccount
	for _, item := range candidates {
		helper := usableCredentialHelper(config.CredentialHelpers[item.server])
		if helper == "" {
			helper = usableCredentialHelper(config.CredentialsStore)
		}
		username, secret, source := "", "", "Docker config"
		if helper != "" {
			output, getErr := runner(helper, "get", []byte(item.server+"\n"))
			if getErr == nil {
				var credential struct {
					Username string
					Secret   string
				}
				if json.Unmarshal(output, &credential) == nil {
					username, secret = credential.Username, credential.Secret
					if username == "<token>" && item.usernameHint != "" {
						username = item.usernameHint
					}
					source = credentialSource(helper)
				}
			}
		}
		if username == "" || secret == "" {
			if auth, ok := config.Auths[item.server]; ok && auth.Auth != "" {
				decoded, decodeErr := base64.StdEncoding.DecodeString(auth.Auth)
				if decodeErr == nil {
					username, secret, _ = strings.Cut(string(decoded), ":")
				}
			}
		}
		username = strings.TrimSpace(username)
		if username == "" || secret == "" {
			continue
		}
		registry := normalizeDiscoveredRegistry(item.server)
		key := discoveryKey(username, registry)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, discoveredAccount{
			Username: username, Registry: registry, ServerURL: item.server,
			Source: source, Secret: secret,
		})
	}
	return result, nil
}

func (a *application) saveDiscoveredCredential(account *accounts.Account, discovered discoveredAccount) error {
	server := canonicalCredentialServer(discovered.Registry)
	helperDir, err := a.prepareCredentialHelper(account)
	if err != nil {
		return err
	}
	if helperDir == "" {
		return a.store.SetInlineCredential(account.Name, server, discovered.Username, discovered.Secret)
	}
	namespaced, err := namespaceServerURL(server, account.Name)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{
		"ServerURL": namespaced,
		"Username":  discovered.Username,
		"Secret":    discovered.Secret,
	})
	if err != nil {
		return err
	}
	if _, err := a.runCredentialCommand(account.CredentialStore, "store", payload); err != nil {
		return err
	}
	if err := a.store.EnsureRegistryAuth(account.Name, server); err != nil {
		_, _ = a.runCredentialCommand(account.CredentialStore, "erase", []byte(namespaced+"\n"))
		return err
	}
	return nil
}

func (a *application) runCredentialCommand(helper, action string, input []byte) ([]byte, error) {
	if a.credentialRunner != nil {
		return a.credentialRunner(helper, action, input)
	}
	return systemCredentialCommand(helper, action, input)
}

func systemCredentialCommand(helper, action string, input []byte) ([]byte, error) {
	command := exec.Command("docker-credential-"+helper, action)
	command.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, errors.New(message)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

func usableCredentialHelper(helper string) string {
	helper = strings.TrimSpace(helper)
	if helper == "" || helper == "docker-account" {
		return ""
	}
	return helper
}

func normalizeDiscoveredRegistry(server string) string {
	if isDockerHubRegistry(server) {
		return accounts.DefaultRegistry
	}
	value := strings.TrimSpace(strings.TrimRight(server, "/"))
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return value
}

func canonicalCredentialServer(registry string) string {
	if isDockerHubRegistry(registry) {
		return "https://index.docker.io/v1/"
	}
	return registry
}

func credentialSource(helper string) string {
	switch helper {
	case "osxkeychain":
		return "macOS Keychain"
	case "wincred":
		return "Windows Credential Manager"
	case "pass":
		return "pass"
	case "secretservice":
		return "Secret Service"
	default:
		return "docker-credential-" + helper
	}
}

func isManagedCredentialServer(server string) bool {
	return strings.Contains(server, "/.docker-account/")
}

func discoveryKey(username, registry string) string {
	return strings.ToLower(strings.TrimSpace(username)) + "\x00" + strings.ToLower(normalizeDiscoveredRegistry(registry))
}

func suggestProfileName(username, registry string) string {
	value := username
	if value == "<token>" || strings.TrimSpace(value) == "" {
		value = normalizeDiscoveredRegistry(registry)
	}
	var result strings.Builder
	lastDash := false
	for _, char := range value {
		allowed := char <= unicode.MaxASCII && (unicode.IsLetter(char) || unicode.IsDigit(char) || char == '.' || char == '_' || char == '-')
		if allowed {
			result.WriteRune(char)
			lastDash = false
		} else if !lastDash {
			result.WriteByte('-')
			lastDash = true
		}
	}
	name := strings.Trim(result.String(), ".-_")
	if name == "" {
		name = "docker"
	}
	if accounts.ValidateName(name) != nil {
		name = "docker-account"
	}
	return name
}

func uniqueProfileName(base string, used map[string]bool) string {
	if !used[base] {
		return base
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s-%d", base, suffix)
		if !used[candidate] {
			return candidate
		}
	}
}

func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
