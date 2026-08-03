package cli

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/pengwu/docker-account/internal/accounts"
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
	options, err := parseAddArgs([]string{"default", "-u", "2030047311", "--password-stdin"})
	if err != nil {
		t.Fatal(err)
	}
	if options.name != "default" || options.username != "2030047311" || !options.passwordStdin {
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
	if got := out.String(); got != "Switched to Docker account \"work\".\n" {
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
	if _, err := store.Add("smartcat999", "bob", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.Use("default"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", store.StableConfigDir())
	t.Setenv("DOCKER_ACCOUNT_NAME", "")
	var out bytes.Buffer
	app := application{store: store, in: strings.NewReader("2\n"), out: &out}
	if err := app.use(nil); err != nil {
		t.Fatal(err)
	}
	current, err := store.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current != "smartcat999" {
		t.Fatalf("current = %q", current)
	}
	for _, expected := range []string{"Select a Docker account:", "default", "smartcat999", `Switched to Docker account "smartcat999".`} {
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
	if got := padBetween("❯ account", "current  ● ready", 36); utf8.RuneCountInString(got) != 36 {
		t.Fatalf("layout width = %d: %q", utf8.RuneCountInString(got), got)
	}
	if got := truncateText("a-very-long-account-name", 10); got != "a-very-lo…" {
		t.Fatalf("truncateText = %q", got)
	}
}

func TestSelectorAccountUsesSingleLine(t *testing.T) {
	item := accounts.Account{Name: "smartcat999", Username: "smartcat99999", Registry: "docker.io"}
	got := selectorAccountText(item, true, true, true, 76)
	if strings.Contains(got, "\n") {
		t.Fatalf("account row contains a newline: %q", got)
	}
	for _, expected := range []string{"❯ smartcat999", "smartcat99999", "docker.io", "current", "● ready"} {
		if !strings.Contains(got, expected) {
			t.Errorf("row missing %q: %q", expected, got)
		}
	}
	if width := utf8.RuneCountInString(got); width != 76 {
		t.Fatalf("row width = %d: %q", width, got)
	}
}

func TestShellQuote(t *testing.T) {
	if got, want := shellQuote("a'b"), `'a'"'"'b'`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
