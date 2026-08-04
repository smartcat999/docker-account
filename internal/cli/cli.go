package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/smartcat999/docker-account/internal/accounts"
	"golang.org/x/term"
)

var errSelectionCanceled = errors.New("account selection canceled")

const (
	selectorBorderStyle   = ""
	selectorAccentStyle   = "32" // the only explicit color
	selectorSelectedStyle = "7"  // terminal-native reverse video
	selectorMutedStyle    = "2"
)

type metadata struct {
	SchemaVersion    string `json:"SchemaVersion"`
	Vendor           string `json:"Vendor"`
	Version          string `json:"Version"`
	ShortDescription string `json:"ShortDescription"`
	URL              string `json:"URL,omitempty"`
}

type application struct {
	store   *accounts.Store
	in      io.Reader
	out     io.Writer
	errOut  io.Writer
	version string
	exe     string
}

type addOptions struct {
	name          string
	username      string
	registry      string
	login         bool
	passwordStdin bool
}

func Run(args []string, in io.Reader, out, errOut io.Writer, version string) int {
	if len(args) > 0 && args[0] == "docker-cli-plugin-metadata" {
		_ = json.NewEncoder(out).Encode(metadata{
			SchemaVersion: "0.1.0", Vendor: "smartcat999", Version: version,
			ShortDescription: "Manage multiple Docker registry accounts",
		})
		return 0
	}
	// Docker invokes a CLI plugin with the plugin name as argv[1]. Running the
	// binary directly does not include it, so support both forms.
	if len(args) > 0 && args[0] == "account" {
		args = args[1:]
	}
	root, err := accounts.DefaultRoot()
	if err != nil {
		fmt.Fprintln(errOut, "Error:", err)
		return 1
	}
	exe, _ := os.Executable()
	app := application{store: accounts.New(root), in: in, out: out, errOut: errOut, version: version, exe: exe}
	if err := app.run(args); err != nil {
		fmt.Fprintln(errOut, "Error:", err)
		return 1
	}
	return 0
}

func (a *application) run(args []string) error {
	if len(args) == 0 {
		a.help()
		return nil
	}
	if len(args) > 1 && wantsHelp(args[1:]) {
		return a.commandHelp(args[0])
	}
	switch args[0] {
	case "help":
		if len(args) == 1 {
			a.help()
			return nil
		}
		if len(args) == 2 {
			return a.commandHelp(args[1])
		}
		return errors.New("usage: docker account help [COMMAND]")
	case "--help", "-h":
		a.help()
		return nil
	case "version", "--version", "-v":
		fmt.Fprintln(a.out, a.version)
		return nil
	case "add":
		return a.add(args[1:])
	case "list", "ls":
		return a.list(args[1:])
	case "current":
		return a.current(args[1:])
	case "use":
		return a.use(args[1:])
	case "login":
		return a.login(args[1:])
	case "logout":
		return a.logout(args[1:])
	case "remove", "rm":
		return a.remove(args[1:])
	case "env":
		return a.env(args[1:])
	case "shell":
		return a.shell(args[1:])
	case "doctor":
		return a.doctor(args[1:])
	default:
		return fmt.Errorf("unknown command %q; run 'docker account --help'", args[0])
	}
}

func (a *application) add(args []string) error {
	options, err := parseAddArgs(args)
	if err != nil {
		return err
	}
	account, err := a.store.Add(options.name, options.username, options.registry)
	if err != nil {
		return err
	}
	if err := a.ensurePlugin(options.name); err != nil {
		return err
	}
	current, err := a.store.Current()
	if err != nil {
		return err
	}
	if current == "" {
		if err := a.store.Use(options.name); err != nil {
			return err
		}
	}
	fmt.Fprintf(a.out, "Added account %q for %s at %s.\n", account.Name, account.Username, account.Registry)
	if options.login || options.passwordStdin {
		return a.loginAccount(account, options.passwordStdin)
	}
	fmt.Fprintf(a.out, "Next: docker account login %s\n", account.Name)
	return nil
}

func parseAddArgs(args []string) (addOptions, error) {
	options := addOptions{registry: accounts.DefaultRegistry}
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--username" || args[i] == "-u":
			if i++; i >= len(args) {
				return addOptions{}, errors.New("--username requires a value")
			}
			options.username = args[i]
		case strings.HasPrefix(args[i], "--username="):
			options.username = strings.TrimPrefix(args[i], "--username=")
		case args[i] == "--registry" || args[i] == "-r":
			if i++; i >= len(args) {
				return addOptions{}, errors.New("--registry requires a value")
			}
			options.registry = args[i]
		case strings.HasPrefix(args[i], "--registry="):
			options.registry = strings.TrimPrefix(args[i], "--registry=")
		case args[i] == "--login":
			options.login = true
		case args[i] == "--password-stdin":
			options.passwordStdin = true
		case strings.HasPrefix(args[i], "-"):
			return addOptions{}, fmt.Errorf("unknown option %q", args[i])
		case options.name == "":
			options.name = args[i]
		default:
			return addOptions{}, errors.New("usage: docker account add NAME --username USER [OPTIONS]")
		}
	}
	if options.name == "" {
		return addOptions{}, errors.New("account name is required")
	}
	if strings.TrimSpace(options.username) == "" {
		return addOptions{}, errors.New("username is required; run 'docker account add --help'")
	}
	return options, nil
}

func (a *application) list(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: docker account list")
	}
	items, err := a.store.List()
	if err != nil {
		return err
	}
	current, err := a.store.Current()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Fprintln(a.out, "No accounts configured.")
		return nil
	}
	w := tabwriter.NewWriter(a.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tUSERNAME\tREGISTRY\tCURRENT\tCREDENTIALS")
	for _, item := range items {
		marker, status := "", "missing"
		if item.Name == current {
			marker = "*"
		}
		if a.store.HasCredentials(item.Name) {
			status = "configured"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", item.Name, item.Username, item.Registry, marker, status)
	}
	return w.Flush()
}

func (a *application) current(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: docker account current")
	}
	name, err := a.store.Current()
	if err != nil {
		return err
	}
	if name == "" {
		return errors.New("no current account; add or select one first")
	}
	fmt.Fprintln(a.out, name)
	return nil
}

func (a *application) use(args []string) error {
	if len(args) > 1 {
		return errors.New("usage: docker account use [NAME]")
	}
	name := ""
	if len(args) == 1 {
		name = args[0]
	} else {
		var err error
		name, err = a.selectAccount()
		if err != nil {
			if errors.Is(err, errSelectionCanceled) {
				return nil
			}
			return err
		}
	}
	account, err := a.store.Get(name)
	if err != nil {
		return accountNotFound(name, err)
	}
	if len(args) == 0 && a.interactiveTerminal() && !a.store.HasCredentials(name) {
		loginNow, err := a.confirmInteractiveLogin(account)
		if err != nil {
			return err
		}
		if !loginNow {
			fmt.Fprintln(a.out, "No account change was made.")
			return nil
		}
		if err := a.ensurePlugin(name); err != nil {
			return err
		}
		if err := a.loginAccount(account, false); err != nil {
			current, _ := a.store.Current()
			if current != "" {
				return fmt.Errorf("active account remains %q: %w", current, err)
			}
			return err
		}
	}
	if err := a.ensurePlugin(name); err != nil {
		return err
	}
	if err := a.store.Use(name); err != nil {
		return err
	}
	if a.shellIntegrationActive() {
		fmt.Fprintf(a.out, "✓ Switched to %s.\n", name)
		return nil
	}
	fmt.Fprintf(a.out, "Selected Docker account %q, but the current shell is not activated.\n", name)
	fmt.Fprintln(a.out, "Run once in this shell: eval \"$(docker account env)\"")
	return nil
}

func (a *application) interactiveTerminal() bool {
	input, inputOK := a.in.(*os.File)
	output, outputOK := a.out.(*os.File)
	return inputOK && outputOK && term.IsTerminal(int(input.Fd())) && term.IsTerminal(int(output.Fd()))
}

func (a *application) confirmInteractiveLogin(account accounts.Account) (bool, error) {
	fmt.Fprintf(a.out, "\nAccount %q is not logged in to %s.\n", account.Name, account.Registry)
	fmt.Fprint(a.out, "Login now? [Y/n] ")
	value, err := bufio.NewReader(a.in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read login confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, errors.New("expected y or n")
	}
}

func (a *application) selectAccount() (string, error) {
	items, err := a.store.List()
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", errors.New("no accounts configured; run 'docker account add --help'")
	}
	current, err := a.store.Current()
	if err != nil {
		return "", err
	}
	currentIndex := -1
	for index, item := range items {
		if item.Name == current {
			currentIndex = index
		}
	}
	if a.interactiveTerminal() {
		return a.selectAccountTTY(a.in.(*os.File), items, currentIndex)
	}
	return a.selectAccountLine(items, currentIndex)
}

func (a *application) selectAccountLine(items []accounts.Account, currentIndex int) (string, error) {
	fmt.Fprintln(a.out, "Select a Docker account:")
	for index, item := range items {
		marker := " "
		if index == currentIndex {
			marker = "*"
		}
		status := "credentials missing"
		if a.store.HasCredentials(item.Name) {
			status = "configured"
		}
		fmt.Fprintf(a.out, "  %s %d) %-16s %s@%s  %s\n", marker, index+1, item.Name, item.Username, item.Registry, status)
	}
	if currentIndex >= 0 {
		fmt.Fprintf(a.out, "Enter number or name [%d]: ", currentIndex+1)
	} else {
		fmt.Fprint(a.out, "Enter number or name: ")
	}
	value, err := bufio.NewReader(a.in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read selection: %w", err)
	}
	value = strings.TrimSpace(value)
	if value == "" && currentIndex >= 0 {
		return items[currentIndex].Name, nil
	}
	if number, err := strconv.Atoi(value); err == nil {
		if number < 1 || number > len(items) {
			return "", fmt.Errorf("selection %d is out of range 1-%d", number, len(items))
		}
		return items[number-1].Name, nil
	}
	for _, item := range items {
		if item.Name == value {
			return item.Name, nil
		}
	}
	return "", fmt.Errorf("account %q is not in the list", value)
}

func (a *application) selectAccountTTY(input *os.File, items []accounts.Account, currentIndex int) (string, error) {
	selected := currentIndex
	if selected < 0 {
		selected = 0
	}
	state, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return "", fmt.Errorf("enable interactive terminal: %w", err)
	}
	defer term.Restore(int(input.Fd()), state)
	fmt.Fprint(a.out, "\x1b[?25l")
	defer fmt.Fprint(a.out, "\x1b[?25h")

	rendered := false
	for {
		a.renderAccountSelector(items, currentIndex, selected, rendered)
		rendered = true
		var key [1]byte
		if _, err := input.Read(key[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return "", errSelectionCanceled
			}
			return "", fmt.Errorf("read terminal selection: %w", err)
		}
		switch key[0] {
		case '\r', '\n':
			return items[selected].Name, nil
		case 3, 'q', 'Q':
			return "", errSelectionCanceled
		case 'j':
			selected = moveSelection(selected, 1, len(items))
		case 'k':
			selected = moveSelection(selected, -1, len(items))
		case 27:
			var sequence [2]byte
			if _, err := io.ReadFull(input, sequence[:]); err != nil {
				return "", errSelectionCanceled
			}
			if sequence[0] != '[' {
				continue
			}
			switch sequence[1] {
			case 'A':
				selected = moveSelection(selected, -1, len(items))
			case 'B':
				selected = moveSelection(selected, 1, len(items))
			}
		}
	}
}

func (a *application) renderAccountSelector(items []accounts.Account, currentIndex, selected int, redraw bool) {
	lineCount := len(items) + 4
	if redraw {
		fmt.Fprintf(a.out, "\x1b[%dA", lineCount)
	}
	showRegistry := false
	for _, item := range items {
		if !isDockerHubRegistry(item.Registry) {
			showRegistry = true
			break
		}
	}
	width := a.selectorWidth()
	maxWidth := 58
	if showRegistry {
		maxWidth = 82
	}
	if width > maxWidth {
		width = maxWidth
	}
	innerWidth := width - 2
	color := os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	border := func(value string) string { return ansiStyle(value, selectorBorderStyle, color) }

	title := " Docker accounts "
	topFill := width - utf8.RuneCountInString(title) - 3
	if topFill < 1 {
		topFill = 1
	}
	top := border("┌─") + ansiStyle(title, selectorAccentStyle, color) + border(strings.Repeat("─", topFill)+"┐")
	a.selectorRawLine(top)
	for index, item := range items {
		nameLine := selectorAccountText(item, index == currentIndex, index == selected, a.store.HasCredentials(item.Name), showRegistry, innerWidth)
		nameStyle := ""
		if index == selected {
			nameStyle = selectorSelectedStyle
		}
		a.selectorBoxLine(nameLine, innerWidth, nameStyle, color)
	}
	a.selectorRawLine(border("├" + strings.Repeat("─", width-2) + "┤"))
	a.selectorBoxLine("  ↑/↓ move   enter switch   q quit", innerWidth, selectorMutedStyle, color)
	a.selectorRawLine(border("└" + strings.Repeat("─", width-2) + "┘"))
}

func selectorAccountText(item accounts.Account, current, selected, configured, showRegistry bool, width int) string {
	selectionMarker := "  "
	if selected {
		selectionMarker = "> "
	}
	prefix := " " + selectionMarker + " "
	status := ""
	if !configured {
		status = "login"
	}
	available := width - utf8.RuneCountInString(status) - 1
	if available < 1 {
		return truncateText(status, width)
	}
	var details string
	switch {
	case showRegistry && width >= 68:
		details = prefix + selectorProfileName(item.Name, current, 16) + "  " + fixedColumn(item.Username, 16) + "  " + item.Registry
	case showRegistry:
		details = prefix + selectorProfileName(item.Name, current, 14) + "  " + item.Registry
	case width >= 44:
		details = prefix + selectorProfileName(item.Name, current, 16) + "  " + item.Username
	default:
		details = prefix + selectorProfileName(item.Name, current, available-utf8.RuneCountInString(prefix))
	}
	details = truncateText(strings.TrimRight(details, " "), available)
	return padBetween(details, status, width)
}

func selectorProfileName(name string, current bool, width int) string {
	if !current {
		return fixedColumn(name, width)
	}
	const activeLabel = " ●"
	nameWidth := width - utf8.RuneCountInString(activeLabel)
	if nameWidth < 1 {
		return fixedColumn("●", width)
	}
	return fixedColumn(truncateText(name, nameWidth)+activeLabel, width)
}

func isDockerHubRegistry(registry string) bool {
	registry = strings.ToLower(strings.TrimSpace(registry))
	registry = strings.TrimSuffix(registry, "/")
	switch registry {
	case "docker.io", "index.docker.io", "registry-1.docker.io", "https://index.docker.io/v1":
		return true
	default:
		return false
	}
}

func fixedColumn(value string, width int) string {
	value = truncateText(value, width)
	padding := width - utf8.RuneCountInString(value)
	if padding < 0 {
		padding = 0
	}
	return value + strings.Repeat(" ", padding)
}

func (a *application) selectorWidth() int {
	width := 68
	if output, ok := a.out.(*os.File); ok {
		if terminalWidth, _, err := term.GetSize(int(output.Fd())); err == nil {
			width = terminalWidth - 2
		}
	}
	if width > 88 {
		width = 88
	}
	if width < 40 {
		width = 40
	}
	return width
}

func (a *application) selectorRawLine(value string) {
	fmt.Fprintf(a.out, "\r\x1b[2K%s\r\n", value)
}

func (a *application) selectorBoxLine(value string, width int, style string, color bool) {
	value = truncateText(value, width)
	padding := width - utf8.RuneCountInString(value)
	if padding < 0 {
		padding = 0
	}
	content := value + strings.Repeat(" ", padding)
	left := ansiStyle("│", selectorBorderStyle, color)
	right := ansiStyle("│", selectorBorderStyle, color)
	fmt.Fprintf(a.out, "\r\x1b[2K%s%s%s\r\n", left, ansiStyle(content, style, color), right)
}

func ansiStyle(value, code string, enabled bool) string {
	if !enabled || code == "" {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func padBetween(left, right string, width int) string {
	right = truncateText(right, width-1)
	rightWidth := utf8.RuneCountInString(right)
	left = truncateText(left, width-rightWidth-1)
	leftWidth := utf8.RuneCountInString(left)
	spaces := width - leftWidth - rightWidth
	if spaces < 1 {
		spaces = 1
	}
	return left + strings.Repeat(" ", spaces) + right
}

func truncateText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func moveSelection(selected, delta, length int) int {
	if length <= 0 {
		return 0
	}
	return (selected + delta + length) % length
}

func (a *application) shellIntegrationActive() bool {
	if strings.TrimSpace(os.Getenv("DOCKER_ACCOUNT_NAME")) != "" {
		return false
	}
	configured := strings.TrimSpace(os.Getenv("DOCKER_CONFIG"))
	if configured == "" {
		return false
	}
	configuredPath, err := filepath.Abs(configured)
	if err != nil {
		return false
	}
	stablePath, err := filepath.Abs(a.store.StableConfigDir())
	if err != nil {
		return false
	}
	return filepath.Clean(configuredPath) == filepath.Clean(stablePath)
}

func (a *application) login(args []string) error {
	name, passwordStdin, err := parseLoginArgs(args)
	if err != nil {
		return err
	}
	account, err := a.store.Get(name)
	if err != nil {
		return accountNotFound(name, err)
	}
	if err := a.ensurePlugin(name); err != nil {
		return err
	}
	return a.loginAccount(account, passwordStdin)
}

func (a *application) loginAccount(account accounts.Account, passwordStdin bool) error {
	helperDir, err := a.prepareCredentialHelper(&account)
	if err != nil {
		return err
	}
	dockerArgs := []string{"--config", a.store.ConfigDir(account.Name), "login", account.Registry, "--username", account.Username}
	if passwordStdin {
		dockerArgs = append(dockerArgs, "--password-stdin")
	}
	environment := accountEnvironment(os.Environ(), account.Name, helperDir)
	if err := a.runCommand("docker", dockerArgs, environment); err != nil {
		return fmt.Errorf("docker login failed: %w", err)
	}
	fmt.Fprintf(a.out, "Credentials saved for account %q. Future account switches do not require login.\n", account.Name)
	return nil
}

func (a *application) logout(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: docker account logout NAME")
	}
	account, err := a.store.Get(args[0])
	if err != nil {
		return accountNotFound(args[0], err)
	}
	return a.logoutAccount(account)
}

func (a *application) logoutAccount(account accounts.Account) error {
	helperDir, err := a.prepareCredentialHelper(&account)
	if err != nil {
		return err
	}
	environment := accountEnvironment(os.Environ(), account.Name, helperDir)
	if err := a.runCommand("docker", []string{"--config", a.store.ConfigDir(account.Name), "logout", account.Registry}, environment); err != nil {
		return fmt.Errorf("docker logout failed: %w", err)
	}
	return nil
}

func (a *application) remove(args []string) error {
	name, force := "", false
	for _, arg := range args {
		switch arg {
		case "--force", "-f":
			force = true
		default:
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("unknown option %q", arg)
			}
			if name != "" {
				return errors.New("usage: docker account remove NAME [--force]")
			}
			name = arg
		}
	}
	if name == "" {
		return errors.New("account name is required")
	}
	account, err := a.store.Get(name)
	if err != nil {
		return accountNotFound(name, err)
	}
	current, err := a.store.Current()
	if err != nil {
		return err
	}
	if current == name && !force {
		return fmt.Errorf("account %q is current; pass --force to remove it", name)
	}
	if a.store.HasCredentials(name) {
		if err := a.logoutAccount(account); err != nil {
			return fmt.Errorf("remove saved credentials before deleting account: %w", err)
		}
	}
	if err := a.store.Remove(name, force); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Removed account %q and its isolated Docker credentials.\n", name)
	return nil
}

func (a *application) env(args []string) error {
	stable := len(args) == 0
	name, err := a.resolveName(args)
	if err != nil {
		return err
	}
	if err := a.ensurePlugin(name); err != nil {
		return err
	}
	account, err := a.store.Get(name)
	if err != nil {
		return err
	}
	helperDir, err := a.prepareCredentialHelper(&account)
	if err != nil {
		return err
	}
	configDir := a.store.ConfigDir(name)
	if stable {
		// Recreate the link for accounts selected by versions before stable shell
		// integration was introduced.
		if err := a.store.Use(name); err != nil {
			return err
		}
		configDir = a.store.StableConfigDir()
		helperDir = a.store.StableHelperDir()
		fmt.Fprintln(a.out, "unset DOCKER_ACCOUNT_NAME")
	} else {
		fmt.Fprintf(a.out, "export DOCKER_ACCOUNT_NAME=%s\n", shellQuote(name))
	}
	fmt.Fprintf(a.out, "export DOCKER_CONFIG=%s\n", shellQuote(configDir))
	if helperDir != "" {
		fmt.Fprintf(a.out, "export PATH=%s:\"$PATH\"\n", shellQuote(helperDir))
	}
	return nil
}

func (a *application) shell(args []string) error {
	name, err := a.resolveName(args)
	if err != nil {
		return err
	}
	if err := a.ensurePlugin(name); err != nil {
		return err
	}
	account, err := a.store.Get(name)
	if err != nil {
		return err
	}
	helperDir, err := a.prepareCredentialHelper(&account)
	if err != nil {
		return err
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	env := accountEnvironment(os.Environ(), name, helperDir)
	env = setEnv(env, "DOCKER_CONFIG", a.store.ConfigDir(name))
	fmt.Fprintf(a.out, "Opening %s with Docker account %q. Exit the shell to restore the previous environment.\n", shell, name)
	return a.runCommand(shell, nil, env)
}

func (a *application) doctor(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: docker account doctor")
	}
	color := terminalOutputColor(a.out)
	fmt.Fprintln(a.out, ansiStyle("Docker account doctor", selectorAccentStyle, color))

	failures := 0
	report := func(symbol, style, message string) {
		fmt.Fprintf(a.out, "%s %s\n", ansiStyle(symbol, style, color), message)
	}
	pass := func(message string) { report("✓", "32", message) }
	warn := func(message string) { report("!", "33", message) }
	fail := func(message string) {
		failures++
		report("✗", "31", message)
	}

	pass("plugin " + a.version)
	if path, err := exec.LookPath("docker"); err != nil {
		fail("Docker CLI not found in PATH")
	} else {
		pass("Docker CLI " + path)
	}

	current, err := a.store.Current()
	if err != nil {
		fail("cannot read current account: " + err.Error())
	} else if current == "" {
		fail("no Docker account selected")
	} else if account, getErr := a.store.Get(current); getErr != nil {
		fail("current account is invalid: " + getErr.Error())
	} else {
		pass(fmt.Sprintf("current account %s (%s@%s)", account.Name, account.Username, account.Registry))
		if a.store.HasCredentials(current) {
			pass("credentials configured")
		} else {
			warn("credentials missing; run docker account login " + current)
		}
	}

	if a.shellIntegrationActive() {
		pass("shell integration active")
	} else {
		warn(`shell integration inactive; run eval "$(docker account env)"`)
	}

	env := setEnv(os.Environ(), "DOCKER_CONFIG", a.store.StableConfigDir())
	if output, commandErr := doctorCommand(env, 5*time.Second, "context", "show"); commandErr != nil {
		fail("Docker context unavailable: " + commandErr.Error())
	} else {
		pass("Docker context " + output)
	}
	if output, commandErr := doctorCommand(env, 8*time.Second, "version", "--format", "{{.Server.Version}}"); commandErr != nil {
		fail("Docker daemon unavailable: " + commandErr.Error())
	} else {
		pass("Docker daemon " + output)
	}
	if output, commandErr := doctorCommand(env, 8*time.Second, "buildx", "inspect"); commandErr != nil {
		fail("Buildx unavailable: " + commandErr.Error())
	} else if name := firstDoctorValue(output, "Name:"); name != "" {
		pass("Buildx builder " + name)
	} else {
		pass("Buildx available")
	}

	if failures > 0 {
		return fmt.Errorf("doctor found %d problem(s)", failures)
	}
	return nil
}

func doctorCommand(environment []string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, "docker", args...)
	command.Env = environment
	output, err := command.CombinedOutput()
	value := strings.TrimSpace(string(output))
	if ctx.Err() == context.DeadlineExceeded {
		return "", errors.New("timed out")
	}
	if err != nil {
		if value != "" {
			return "", errors.New(firstLine(value))
		}
		return "", err
	}
	return value, nil
}

func firstDoctorValue(output, prefix string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func firstLine(value string) string {
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		return value[:index]
	}
	return value
}

func terminalOutputColor(output io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := output.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func (a *application) resolveName(args []string) (string, error) {
	if len(args) > 1 {
		return "", errors.New("expected zero or one account name")
	}
	name := ""
	if len(args) == 1 {
		name = args[0]
	}
	if name == "" {
		var err error
		name, err = a.store.Current()
		if err != nil {
			return "", err
		}
	}
	if name == "" {
		return "", errors.New("no current account; add or select one first")
	}
	if _, err := a.store.Get(name); err != nil {
		return "", accountNotFound(name, err)
	}
	return name, nil
}

func (a *application) ensurePlugin(name string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve Docker CLI plugin directory: %w", err)
	}
	dockerDir := filepath.Join(home, ".docker")
	if err := a.store.InheritDockerRuntimeConfig(name, dockerDir); err != nil {
		return err
	}
	if err := a.store.ShareDockerRuntimeState(name, dockerDir); err != nil {
		return err
	}
	if a.exe == "" {
		return nil
	}
	if err := a.store.EnsurePlugin(name, a.exe); err != nil {
		return err
	}
	return a.store.ConfigureCLIPluginDir(name, filepath.Join(dockerDir, "cli-plugins"))
}

func (a *application) prepareCredentialHelper(account *accounts.Account) (string, error) {
	helper := account.CredentialStore
	if helper == "" {
		helper = detectNativeCredentialStore()
		if helper == "" {
			return "", nil
		}
		updated, err := a.store.SetCredentialStore(account.Name, helper)
		if err != nil {
			return "", err
		}
		*account = updated
	}
	if helper == "docker-account" {
		return "", errors.New("native credential store cannot refer to docker-account itself")
	}
	if _, err := exec.LookPath("docker-credential-" + helper); err != nil {
		return "", fmt.Errorf("native credential helper docker-credential-%s is unavailable: %w", helper, err)
	}
	if err := a.store.ConfigureDockerCredentialStore(account.Name, "docker-account"); err != nil {
		return "", err
	}
	return a.store.EnsureCredentialHelper(account.Name, a.exe)
}

func detectNativeCredentialStore() string {
	if helper := strings.TrimSpace(os.Getenv("DOCKER_ACCOUNT_CREDENTIAL_STORE")); helper != "" {
		return helper
	}
	if home, err := os.UserHomeDir(); err == nil {
		data, err := os.ReadFile(filepath.Join(home, ".docker", "config.json"))
		if err == nil {
			var config struct {
				CredentialsStore string `json:"credsStore"`
			}
			if json.Unmarshal(data, &config) == nil && config.CredentialsStore != "" && config.CredentialsStore != "docker-account" {
				if _, err := exec.LookPath("docker-credential-" + config.CredentialsStore); err == nil {
					return config.CredentialsStore
				}
			}
		}
	}
	candidates := map[string][]string{
		"darwin":  {"osxkeychain"},
		"windows": {"wincred"},
		"linux":   {"pass", "secretservice"},
	}
	for _, helper := range candidates[runtime.GOOS] {
		if _, err := exec.LookPath("docker-credential-" + helper); err == nil {
			return helper
		}
	}
	return ""
}

func (a *application) runCommand(name string, args []string, environment []string) error {
	command := exec.Command(name, args...)
	command.Stdin, command.Stdout, command.Stderr = a.in, a.out, a.errOut
	if environment != nil {
		command.Env = environment
	}
	return command.Run()
}

func (a *application) help() {
	fmt.Fprint(a.out, `Usage:  docker account COMMAND

Manage multiple Docker registry accounts with isolated credentials.

Commands:
  add       Add an account definition
  list      List managed accounts
  current   Print the selected account
  use       Select the default account (does not log in again)
  login     Log in and persist credentials for one account
  logout    Remove persisted credentials for one account
  remove    Remove an account and its isolated credentials
  env       Print shell exports for the selected account
  shell     Open a child shell using the selected account
  doctor    Diagnose account, Docker, context, and Buildx setup
  version   Print the plugin version

Examples:
  docker account add personal --username my-user
  docker account login personal
  docker account use personal
  eval "$(docker account env)"
  docker account shell personal
`)
}

func (a *application) commandHelp(command string) error {
	var help string
	switch command {
	case "add":
		help = `Usage:  docker account add NAME --username USER [OPTIONS]

Add an account definition and optionally log in immediately.

Options:
  -u, --username USER       Registry username (required)
  -r, --registry REGISTRY   Registry hostname (default: docker.io)
      --login               Prompt for a password or access token after adding
      --password-stdin      Read a password or access token from standard input
  -h, --help                Show this help

Examples:
  docker account add personal --username your-docker-id
  docker account add personal --username your-docker-id --login
  printf '%s' "$DOCKER_HUB_TOKEN" | docker account add personal -u your-docker-id --password-stdin
`
	case "list", "ls":
		help = `Usage:  docker account list

List managed accounts and their local credential status.
`
	case "current":
		help = `Usage:  docker account current

Print the currently selected account name.
`
	case "use":
		help = `Usage:  docker account use [NAME]

Select the default account. With no NAME, open an interactive selector.
This never performs a login.

Enable automatic switching once in Zsh:
  echo 'eval "$(docker account env)"' >> ~/.zshrc
  source ~/.zshrc
`
	case "login":
		help = `Usage:  docker account login NAME [OPTIONS]

Log in once and save credentials in the account's isolated Docker config.

Options:
      --password-stdin   Read a password or access token from standard input
  -h, --help             Show this help

Examples:
  docker account login personal
  printf '%s' "$DOCKER_HUB_TOKEN" | docker account login personal --password-stdin
`
	case "logout":
		help = `Usage:  docker account logout NAME

Remove the saved registry credentials for an account without deleting it.
`
	case "remove", "rm":
		help = `Usage:  docker account remove NAME [OPTIONS]

Remove an account definition and its isolated Docker credentials.

Options:
  -f, --force   Allow removal of the currently selected account
  -h, --help    Show this help
`
	case "env":
		help = `Usage:  docker account env [NAME]

Without NAME, print shell exports that use the stable current-account link.
After evaluating them once, future 'docker account use' commands switch automatically.
With NAME, print exports pinned directly to that specific account.

Example:
  eval "$(docker account env company)"
`
	case "shell":
		help = `Usage:  docker account shell [NAME]

Open a child shell with the selected account's isolated DOCKER_CONFIG.
Exit the child shell to restore the previous environment.
`
	case "version":
		help = `Usage:  docker account version

Print the Docker CLI plugin version.
`
	case "doctor":
		help = `Usage:  docker account doctor

Check the selected account, credentials, shell integration, Docker context,
daemon connection, and active Buildx builder.
`
	default:
		return fmt.Errorf("unknown command %q; run 'docker account --help'", command)
	}
	fmt.Fprint(a.out, help)
	return nil
}

func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func parseLoginArgs(args []string) (string, bool, error) {
	name, passwordStdin := "", false
	for _, arg := range args {
		switch arg {
		case "--password-stdin":
			passwordStdin = true
		default:
			if strings.HasPrefix(arg, "-") {
				return "", false, fmt.Errorf("unknown option %q", arg)
			}
			if name != "" {
				return "", false, errors.New("usage: docker account login NAME [--password-stdin]")
			}
			name = arg
		}
	}
	if name == "" {
		return "", false, errors.New("account name is required")
	}
	return name, passwordStdin, nil
}

func accountNotFound(name string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("account %q does not exist", name)
	}
	return err
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func setEnv(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}

func accountEnvironment(environment []string, name, helperDir string) []string {
	environment = setEnv(environment, "DOCKER_ACCOUNT_NAME", name)
	if helperDir == "" {
		return environment
	}
	path := envValue(environment, "PATH")
	return setEnv(environment, "PATH", helperDir+string(os.PathListSeparator)+path)
}

func envValue(environment []string, key string) string {
	prefix := key + "="
	for _, item := range environment {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}
