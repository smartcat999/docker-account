package accounts

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const DefaultRegistry = "docker.io"

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Account struct {
	Name            string    `json:"name"`
	Username        string    `json:"username"`
	Registry        string    `json:"registry"`
	CredentialStore string    `json:"credentialStore,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type state struct {
	Version int               `json:"version,omitempty"`
	Current string            `json:"current,omitempty"`
	Active  map[string]string `json:"active,omitempty"`
}

type Store struct {
	Root string
	Now  func() time.Time
}

func DefaultRoot() (string, error) {
	if root := strings.TrimSpace(os.Getenv("DOCKER_ACCOUNT_HOME")); root != "" {
		return filepath.Abs(root)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".docker", "accounts"), nil
}

func New(root string) *Store {
	return &Store{Root: root, Now: time.Now}
}

func ValidateName(name string) error {
	if !validName.MatchString(name) || name == "." || name == ".." || name == "current" {
		return fmt.Errorf("invalid account name %q: use letters, numbers, dot, underscore, or hyphen", name)
	}
	return nil
}

func (s *Store) Add(name, username, registry string) (Account, error) {
	if err := ValidateName(name); err != nil {
		return Account{}, err
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return Account{}, errors.New("username is required")
	}
	registry = strings.TrimSpace(registry)
	if registry == "" {
		registry = DefaultRegistry
	}
	if _, err := s.Get(name); err == nil {
		return Account{}, fmt.Errorf("account %q already exists", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Account{}, err
	}

	now := s.Now().UTC()
	account := Account{Name: name, Username: username, Registry: registry, CreatedAt: now, UpdatedAt: now}
	if err := os.MkdirAll(s.ConfigDir(name), 0o700); err != nil {
		return Account{}, fmt.Errorf("create account directory: %w", err)
	}
	if err := writeJSONAtomic(s.accountPath(name), account); err != nil {
		return Account{}, err
	}
	return account, nil
}

func (s *Store) Get(name string) (Account, error) {
	if err := ValidateName(name); err != nil {
		return Account{}, err
	}
	var account Account
	if err := readJSON(s.accountPath(name), &account); err != nil {
		return Account{}, err
	}
	return account, nil
}

func (s *Store) List() ([]Account, error) {
	entries, err := os.ReadDir(s.Root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	result := make([]Account, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || ValidateName(entry.Name()) != nil {
			continue
		}
		account, err := s.Get(entry.Name())
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read account %q: %w", entry.Name(), err)
		}
		result = append(result, account)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (s *Store) Current() (string, error) {
	value, err := s.loadState()
	if err != nil {
		return "", err
	}
	return value.Current, nil
}

func (s *Store) Use(name string) error {
	account, err := s.Get(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("account %q does not exist", name)
		}
		return err
	}
	value, err := s.loadState()
	if err != nil {
		return err
	}
	if value.Active == nil {
		value.Active = map[string]string{}
	}
	value.Version = 2
	value.Current = name
	value.Active[RegistryKey(account.Registry)] = name
	if err := s.updateCurrentLink(name); err != nil {
		return err
	}
	return writeJSONAtomic(s.statePath(), value)
}

// ActivateIfUnset makes the first account for a registry available without
// changing the profile used by the stable current-account link.
func (s *Store) ActivateIfUnset(name string) (bool, error) {
	account, err := s.Get(name)
	if err != nil {
		return false, err
	}
	value, err := s.loadState()
	if err != nil {
		return false, err
	}
	key := RegistryKey(account.Registry)
	if value.Active[key] != "" {
		return false, nil
	}
	if value.Active == nil {
		value.Active = map[string]string{}
	}
	value.Version = 2
	value.Active[key] = name
	if value.Current == "" {
		value.Current = name
		if err := s.updateCurrentLink(name); err != nil {
			return false, err
		}
	}
	return true, writeJSONAtomic(s.statePath(), value)
}

func (s *Store) ActiveName(registry string) (string, error) {
	value, err := s.loadState()
	if err != nil {
		return "", err
	}
	return value.Active[RegistryKey(registry)], nil
}

func (s *Store) ActiveAccounts() (map[string]string, error) {
	value, err := s.loadState()
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(value.Active))
	for registry, name := range value.Active {
		result[registry] = name
	}
	return result, nil
}

func (s *Store) IsActive(name string) (bool, error) {
	active, err := s.ActiveAccounts()
	if err != nil {
		return false, err
	}
	for _, activeName := range active {
		if activeName == name {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) loadState() (state, error) {
	var value state
	if err := readJSON(s.statePath(), &value); err != nil && !errors.Is(err, os.ErrNotExist) {
		return state{}, err
	}
	if value.Active == nil {
		value.Active = map[string]string{}
	}
	items, err := s.List()
	if err != nil {
		return state{}, err
	}
	groups := map[string][]Account{}
	for _, item := range items {
		key := RegistryKey(item.Registry)
		groups[key] = append(groups[key], item)
		if item.Name == value.Current {
			value.Active[key] = item.Name
		}
	}
	for key, group := range groups {
		if len(group) == 1 && value.Active[key] == "" {
			value.Active[key] = group[0].Name
		}
	}
	return value, nil
}

func RegistryKey(registry string) string {
	value := strings.ToLower(strings.TrimSpace(strings.TrimRight(registry, "/")))
	switch value {
	case "docker.io", "index.docker.io", "registry-1.docker.io", "https://index.docker.io/v1":
		return DefaultRegistry
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return value
}

func (s *Store) SetCredentialStore(name, helper string) (Account, error) {
	account, err := s.Get(name)
	if err != nil {
		return Account{}, err
	}
	account.CredentialStore = helper
	account.UpdatedAt = s.Now().UTC()
	if err := writeJSONAtomic(s.accountPath(name), account); err != nil {
		return Account{}, err
	}
	return account, nil
}

func (s *Store) ConfigureDockerCredentialStore(name, helper string) error {
	return s.updateDockerConfig(name, func(config map[string]any) {
		if _, ok := config["auths"]; !ok {
			config["auths"] = map[string]any{}
		}
		config["credsStore"] = helper
	})
}

func (s *Store) EnsureRegistryAuth(name, registry string) error {
	return s.updateDockerConfig(name, func(config map[string]any) {
		auths, ok := config["auths"].(map[string]any)
		if !ok {
			auths = map[string]any{}
			config["auths"] = auths
		}
		if _, exists := auths[registry]; !exists {
			auths[registry] = map[string]any{}
		}
	})
}

func (s *Store) SetInlineCredential(name, registry, username, secret string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + secret))
	return s.updateDockerConfig(name, func(config map[string]any) {
		auths, ok := config["auths"].(map[string]any)
		if !ok {
			auths = map[string]any{}
			config["auths"] = auths
		}
		auths[registry] = map[string]any{"auth": encoded}
		delete(config, "credsStore")
	})
}

func (s *Store) ConfigureCLIPluginDir(name, dir string) error {
	dir = filepath.Clean(dir)
	return s.updateDockerConfig(name, func(config map[string]any) {
		dirs := make([]string, 0, 2)
		seen := map[string]bool{}
		if existing, ok := config["cliPluginsExtraDirs"].([]any); ok {
			for _, value := range existing {
				if item, ok := value.(string); ok && item != "" && !seen[item] {
					dirs = append(dirs, item)
					seen[item] = true
				}
			}
		}
		if !seen[dir] {
			dirs = append(dirs, dir)
		}
		config["cliPluginsExtraDirs"] = dirs
	})
}

// InheritDockerRuntimeConfig copies Docker CLI settings from the user's main
// config while retaining the account-specific credential settings.
func (s *Store) InheritDockerRuntimeConfig(name, dockerDir string) error {
	sourcePath := filepath.Join(dockerDir, "config.json")
	source := map[string]any{}
	if data, err := os.ReadFile(sourcePath); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &source); err != nil {
			return fmt.Errorf("decode %s: %w", sourcePath, err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return s.updateDockerConfig(name, func(account map[string]any) {
		for key, value := range source {
			if !accountConfigKey(key) {
				account[key] = value
			}
		}
	})
}

func accountConfigKey(key string) bool {
	switch key {
	case "auths", "credsStore", "credHelpers", "cliPluginsExtraDirs":
		return true
	default:
		return false
	}
}

// ShareDockerRuntimeState makes contexts and Buildx builders independent of
// the selected registry account. Existing account-local state is preserved as
// a timestamped backup before it is replaced by a symlink.
func (s *Store) ShareDockerRuntimeState(name, dockerDir string) error {
	for _, entry := range []string{"contexts", "buildx"} {
		source := filepath.Join(dockerDir, entry)
		if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect shared Docker %s: %w", entry, err)
		}
		if err := s.ensureSharedPath(name, entry, source); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureSharedPath(name, entry, source string) error {
	target := filepath.Join(s.ConfigDir(name), entry)
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		wanted, resolveErr := filepath.EvalSymlinks(source)
		if resolveErr == nil && resolved == wanted {
			return nil
		}
	}

	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(target); err != nil {
				return fmt.Errorf("replace Docker %s link: %w", entry, err)
			}
		} else {
			stamp := s.Now().UTC().Format("20060102T150405Z")
			backup := target + ".account-backup-" + stamp
			for suffix := 1; ; suffix++ {
				if _, err := os.Lstat(backup); errors.Is(err, os.ErrNotExist) {
					break
				}
				backup = fmt.Sprintf("%s.account-backup-%s-%d", target, stamp, suffix)
			}
			if err := os.Rename(target, backup); err != nil {
				return fmt.Errorf("preserve account Docker %s state: %w", entry, err)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect account Docker %s: %w", entry, err)
	}

	if err := os.Symlink(source, target); err != nil {
		return fmt.Errorf("share Docker %s state: %w", entry, err)
	}
	return nil
}

func (s *Store) updateDockerConfig(name string, update func(map[string]any)) error {
	path := filepath.Join(s.ConfigDir(name), "config.json")
	config := map[string]any{}
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	update(config)
	return writeJSONAtomic(path, config)
}

func (s *Store) Remove(name string, force bool) error {
	if _, err := s.Get(name); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("account %q does not exist", name)
		}
		return err
	}
	value, err := s.loadState()
	if err != nil {
		return err
	}
	active := false
	for _, activeName := range value.Active {
		active = active || activeName == name
	}
	if active && !force {
		return fmt.Errorf("account %q is active; pass --force to remove it", name)
	}
	if err := os.RemoveAll(s.accountDir(name)); err != nil {
		return fmt.Errorf("remove account %q: %w", name, err)
	}
	for registry, activeName := range value.Active {
		if activeName == name {
			delete(value.Active, registry)
		}
	}
	if value.Current == name {
		value.Current = ""
		remaining, listErr := s.List()
		if listErr != nil {
			return listErr
		}
		for _, item := range remaining {
			if value.Active[RegistryKey(item.Registry)] == item.Name {
				value.Current = item.Name
				break
			}
		}
		if value.Current != "" {
			if err := s.updateCurrentLink(value.Current); err != nil {
				return err
			}
		} else if err := os.Remove(s.CurrentLink()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove current account link: %w", err)
		}
	}
	value.Version = 2
	return writeJSONAtomic(s.statePath(), value)
}

func (s *Store) ConfigDir(name string) string {
	return filepath.Join(s.accountDir(name), "docker")
}

func (s *Store) CurrentLink() string {
	return filepath.Join(s.Root, "current")
}

func (s *Store) StableConfigDir() string {
	return filepath.Join(s.CurrentLink(), "docker")
}

func (s *Store) StableHelperDir() string {
	return filepath.Join(s.StableConfigDir(), "bin")
}

func (s *Store) HasCredentials(name string) bool {
	file, err := os.Open(filepath.Join(s.ConfigDir(name), "config.json"))
	if err != nil {
		return false
	}
	defer file.Close()
	var config struct {
		Auths map[string]json.RawMessage `json:"auths"`
	}
	return json.NewDecoder(file).Decode(&config) == nil && len(config.Auths) > 0
}

func (s *Store) EnsurePlugin(name, executable string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err == nil {
		executable = resolved
	}
	dir := filepath.Join(s.ConfigDir(name), "cli-plugins")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create plugin directory: %w", err)
	}
	target := filepath.Join(dir, "docker-account")
	if current, err := filepath.EvalSymlinks(target); err == nil && current == executable {
		return nil
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("replace plugin link: %w", err)
	}
	if err := os.Symlink(executable, target); err == nil {
		return nil
	}
	return copyExecutable(executable, target)
}

func (s *Store) EnsureCredentialHelper(name, executable string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err == nil {
		executable = resolved
	}
	dir := filepath.Join(s.ConfigDir(name), "bin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create credential helper directory: %w", err)
	}
	target := filepath.Join(dir, "docker-credential-docker-account")
	if current, err := filepath.EvalSymlinks(target); err == nil && current == executable {
		return dir, nil
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("replace credential helper link: %w", err)
	}
	if err := os.Symlink(executable, target); err == nil {
		return dir, nil
	}
	if err := copyExecutable(executable, target); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *Store) accountDir(name string) string {
	return filepath.Join(s.Root, name)
}

func (s *Store) accountPath(name string) string {
	return filepath.Join(s.accountDir(name), "account.json")
}

func (s *Store) statePath() string {
	return filepath.Join(s.Root, "state.json")
}

func (s *Store) updateCurrentLink(name string) error {
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return fmt.Errorf("create account root: %w", err)
	}
	link := s.CurrentLink()
	if info, err := os.Lstat(link); err == nil && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("cannot update %s: path exists and is not a symbolic link", link)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temp, err := os.CreateTemp(s.Root, ".current-link-*")
	if err != nil {
		return fmt.Errorf("create temporary current link: %w", err)
	}
	tempPath := temp.Name()
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Remove(tempPath); err != nil {
		return err
	}
	defer os.Remove(tempPath)
	if err := os.Symlink(s.accountDir(name), tempPath); err != nil {
		return fmt.Errorf("create current account link: %w", err)
	}
	if err := os.Rename(tempPath, link); err != nil {
		return fmt.Errorf("activate account %q: %w", name, err)
	}
	return nil
}

func readJSON(path string, value any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := json.NewDecoder(file).Decode(value); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	encoder := json.NewEncoder(temp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		temp.Close()
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func copyExecutable(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open plugin executable: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("create plugin executable: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy plugin executable: %w", err)
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil
}
