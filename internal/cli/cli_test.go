package cli

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/smartcat999/docker-account/internal/accounts"
)

func TestMetadata(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run([]string{"docker-cli-plugin-metadata"}, strings.NewReader(""), &out, &errOut, "v1.2.3")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, errOut.String())
	}
	for _, expected := range []string{`"SchemaVersion":"0.1.0"`, `"Version":"v1.2.3"`} {
		if !strings.Contains(out.String(), expected) {
			t.Errorf("metadata missing %s: %s", expected, out.String())
		}
	}
}

func TestDockerPluginInvocationStripsPluginName(t *testing.T) {
	t.Setenv("DOCKER_ACCOUNT_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := Run([]string{"account", "version"}, strings.NewReader(""), &out, &errOut, "v1.2.3")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "v1.2.3" {
		t.Fatalf("version = %q", got)
	}
}

func TestSubcommandHelpAfterPositionalArgument(t *testing.T) {
	t.Setenv("DOCKER_ACCOUNT_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := Run([]string{"account", "add", "default", "-h"}, strings.NewReader(""), &out, &errOut, "dev")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, errOut.String())
	}
	for _, expected := range []string{"docker account add NAME", "--username USER", "--registry REGISTRY", "--login", "--password-stdin"} {
		if !strings.Contains(out.String(), expected) {
			t.Errorf("help missing %q:\n%s", expected, out.String())
		}
	}
}

func TestParseAddArgsWithLogin(t *testing.T) {
	options, err := parseAddArgs([]string{"personal", "-u", "alice", "--password-stdin"})
	if err != nil {
		t.Fatal(err)
	}
	if options.name != "personal" || options.username != "alice" || !options.passwordStdin {
		t.Fatalf("unexpected options: %+v", options)
	}
}

func TestParseAddArgsRequiresUsername(t *testing.T) {
	_, err := parseAddArgs([]string{"default"})
	if err == nil || !strings.Contains(err.Error(), "--help") {
		t.Fatalf("expected actionable username error, got %v", err)
	}
}

func TestHelpCommandForSubcommand(t *testing.T) {
	t.Setenv("DOCKER_ACCOUNT_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := Run([]string{"help", "login"}, strings.NewReader(""), &out, &errOut, "dev")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "--password-stdin") {
		t.Fatalf("unexpected help:\n%s", out.String())
	}
}

func TestUseOutputWhenShellIntegrationIsActive(t *testing.T) {
	store := accounts.New(t.TempDir())
	if _, err := store.Add("work", "alice", ""); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", store.StableConfigDir())
	t.Setenv("DOCKER_ACCOUNT_NAME", "")
	var out bytes.Buffer
	app := application{store: store, out: &out}
	if err := app.use([]string{"work"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "✓ Switched to work.\n" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestUseWarnsWhenShellIntegrationIsInactive(t *testing.T) {
	store := accounts.New(t.TempDir())
	if _, err := store.Add("work", "alice", ""); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", "")
	t.Setenv("DOCKER_ACCOUNT_NAME", "")
	var out bytes.Buffer
	app := application{store: store, out: &out}
	if err := app.use([]string{"work"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "current shell is not activated") || !strings.Contains(got, "eval") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestUseInteractiveSelectionByNumber(t *testing.T) {
	store := accounts.New(t.TempDir())
	if _, err := store.Add("default", "alice", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("company", "bob", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.Use("default"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", store.StableConfigDir())
	t.Setenv("DOCKER_ACCOUNT_NAME", "")
	var out bytes.Buffer
	app := application{store: store, in: strings.NewReader("1\n"), out: &out}
	if err := app.use(nil); err != nil {
		t.Fatal(err)
	}
	current, err := store.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current != "company" {
		t.Fatalf("current = %q", current)
	}
	for _, expected := range []string{"Select a Docker account:", "default", "company", "✓ Switched to company."} {
		if !strings.Contains(out.String(), expected) {
			t.Errorf("output missing %q:\n%s", expected, out.String())
		}
	}
}

func TestUseInteractiveDefaultsToCurrentAccount(t *testing.T) {
	store := accounts.New(t.TempDir())
	if _, err := store.Add("work", "alice", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.Use("work"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", store.StableConfigDir())
	t.Setenv("DOCKER_ACCOUNT_NAME", "")
	var out bytes.Buffer
	app := application{store: store, in: strings.NewReader("\n"), out: &out}
	if err := app.use(nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Enter number or name [1]") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestMoveSelectionWraps(t *testing.T) {
	if got := moveSelection(0, -1, 2); got != 1 {
		t.Fatalf("move up from first = %d", got)
	}
	if got := moveSelection(1, 1, 2); got != 0 {
		t.Fatalf("move down from last = %d", got)
	}
}

func TestSelectorTextLayout(t *testing.T) {
	if got := padBetween(" >  account ●", "login", 36); utf8.RuneCountInString(got) != 36 {
		t.Fatalf("layout width = %d: %q", utf8.RuneCountInString(got), got)
	}
	if got := truncateText("a-very-long-account-name", 10); got != "a-very-lo…" {
		t.Fatalf("truncateText = %q", got)
	}
}

func TestSelectorAccountUsesSingleLine(t *testing.T) {
	item := accounts.Account{Name: "company", Username: "alice", Registry: "registry.example.com"}
	got := selectorAccountText(item, true, true, true, true, 76)
	if strings.Contains(got, "\n") {
		t.Fatalf("account row contains a newline: %q", got)
	}
	for _, expected := range []string{">  company ●", "alice", "registry.example.com"} {
		if !strings.Contains(got, expected) {
			t.Errorf("row missing %q: %q", expected, got)
		}
	}
	if strings.Contains(got, "login") {
		t.Fatalf("healthy row displays a redundant status: %q", got)
	}
	if width := utf8.RuneCountInString(got); width != 76 {
		t.Fatalf("row width = %d: %q", width, got)
	}
}

func TestSelectorHidesDockerHubRegistryAndHealthyStatus(t *testing.T) {
	item := accounts.Account{Name: "personal", Username: "demo-user", Registry: "docker.io"}
	got := selectorAccountText(item, true, false, true, false, 56)
	if strings.Contains(got, "docker.io") || strings.Contains(got, "Ready") {
		t.Fatalf("row contains redundant Docker Hub information: %q", got)
	}
	for _, expected := range []string{"personal ●", "demo-user"} {
		if !strings.Contains(got, expected) {
			t.Errorf("row missing %q: %q", expected, got)
		}
	}
}

func TestSelectorShowsOnlyExceptionalStatus(t *testing.T) {
	item := accounts.Account{Name: "company", Username: "alice", Registry: "docker.io"}
	got := selectorAccountText(item, false, true, false, false, 56)
	for _, expected := range []string{">", "company", "alice", "login"} {
		if !strings.Contains(got, expected) {
			t.Errorf("row missing %q: %q", expected, got)
		}
	}
}

func TestDockerHubRegistryAliases(t *testing.T) {
	for _, registry := range []string{"docker.io", "index.docker.io", "registry-1.docker.io", "https://index.docker.io/v1/"} {
		if !isDockerHubRegistry(registry) {
			t.Errorf("expected Docker Hub registry: %q", registry)
		}
	}
	if isDockerHubRegistry("registry.example.com") {
		t.Fatal("private registry identified as Docker Hub")
	}
}

func TestConfirmInteractiveLogin(t *testing.T) {
	account := accounts.Account{Name: "company", Registry: "registry.example.com"}
	for _, test := range []struct {
		input string
		want  bool
	}{
		{"\n", true},
		{"yes\n", true},
		{"n\n", false},
	} {
		var out bytes.Buffer
		app := application{in: strings.NewReader(test.input), out: &out}
		got, err := app.confirmInteractiveLogin(account)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("input %q: got %v, want %v", test.input, got, test.want)
		}
	}
}

func TestSelectorUsesClassicANSIPalette(t *testing.T) {
	store := accounts.New(t.TempDir())
	if _, err := store.Add("work", "alice", ""); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	var out bytes.Buffer
	app := application{store: store, out: &out}
	app.renderAccountSelector([]accounts.Account{{Name: "work", Username: "alice", Registry: "docker.io"}}, 0, 0, false)

	got := out.String()
	for _, expected := range []string{"\x1b[" + selectorAccentStyle + "m", "\x1b[" + selectorSelectedStyle + "m", "\x1b[" + selectorMutedStyle + "m"} {
		if !strings.Contains(got, expected) {
			t.Errorf("selector output missing classic ANSI style %q: %q", expected, got)
		}
	}
	if strings.Contains(got, "38;5;45") || strings.Contains(got, "48;5;45") {
		t.Fatalf("selector still contains cyan styling: %q", got)
	}
	if strings.Contains(got, "38;5;") || strings.Contains(got, "48;5;") {
		t.Fatalf("selector uses fixed 256-color styling: %q", got)
	}
	if strings.Contains(got, "\x1b[31m") || strings.Contains(got, "\x1b[34m") || strings.Contains(got, "\x1b[35m") || strings.Contains(got, "\x1b[36m") {
		t.Fatalf("selector uses more than one explicit color: %q", got)
	}
}

func TestDoctorOutputParsing(t *testing.T) {
	output := "Name:          my-builder\nDriver:        docker-container\n"
	if got := firstDoctorValue(output, "Name:"); got != "my-builder" {
		t.Fatalf("builder name = %q", got)
	}
	if got := firstLine("connection failed\nmore details"); got != "connection failed" {
		t.Fatalf("first line = %q", got)
	}
}

func TestDoctorHelp(t *testing.T) {
	t.Setenv("DOCKER_ACCOUNT_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := Run([]string{"doctor", "--help"}, strings.NewReader(""), &out, &errOut, "dev")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, errOut.String())
	}
	for _, expected := range []string{"docker account doctor", "Docker context", "Buildx"} {
		if !strings.Contains(out.String(), expected) {
			t.Errorf("help missing %q: %s", expected, out.String())
		}
	}
}

func TestShellQuote(t *testing.T) {
	if got, want := shellQuote("a'b"), `'a'"'"'b'`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
