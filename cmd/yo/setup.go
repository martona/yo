// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/term"

	"github.com/martona/yo/internal/config"
	"github.com/martona/yo/internal/session"
	tokens "github.com/martona/yo/internal/usage"
	"github.com/martona/yo/shell"
)

type setupRunner struct {
	in     *bufio.Reader
	inFile *os.File
	out    io.Writer
	err    io.Writer
}

var (
	powerShellSetupHost        = detectPowerShellSetupHost
	shouldOfferPowerShellSetup = defaultShouldOfferPowerShellSetup
)

func runSetup(uninstall bool) {
	if err := newSetupRunner(os.Stdin, os.Stdout, os.Stderr).run(uninstall); err != nil {
		fmt.Fprintln(os.Stderr, "yo:", err)
		os.Exit(1)
	}
}

func newSetupRunner(in io.Reader, out, err io.Writer) *setupRunner {
	s := &setupRunner{in: bufio.NewReader(in), out: out, err: err}
	if f, ok := in.(*os.File); ok {
		s.inFile = f
	}
	return s
}

func (s *setupRunner) run(uninstall bool) error {
	if uninstall {
		if err := s.uninstallShells(); err != nil {
			return err
		}
		s.removeStateFiles()
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate the yo binary: %w", err)
	}

	fmt.Fprintln(s.out)
	fmt.Fprintln(s.out, "yo setup -- I'll ask before each change. Press Enter to accept, 'n' to skip.")
	fmt.Fprintln(s.out, "Skipping a step is fine; setup keeps going.")

	if err := s.installShells(exe); err != nil {
		return err
	}
	if err := s.configureKey(); err != nil {
		return err
	}
	s.validate(setupTargetShell())

	fmt.Fprintln(s.out)
	fmt.Fprintln(s.out, "Setup complete. Open a new shell (or run the init line now), then try:  yo list files over 100mb")
	return nil
}

func (s *setupRunner) installShells(exe string) error {
	if shouldOfferPowerShellSetup() {
		if err := s.runPowerShellShellSetup(exe, false); err != nil {
			return err
		}
	}
	localBin, needsPath, ok, err := s.ensurePosixBinaryOnPath(exe)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	for _, profile := range posixShellProfiles() {
		if needsPath {
			ok, err := s.ensureLocalBinOnProfilePath(profile, localBin)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
		}
		if err := s.wirePosixProfile(profile, exe); err != nil {
			return err
		}
	}
	return nil
}

func (s *setupRunner) installShell(target, exe string) error {
	switch target {
	case "bash":
		return s.wireBashProfile(exe)
	case "zsh":
		return s.wireZshProfile(exe)
	default:
		return s.runPowerShellShellSetup(exe, false)
	}
}

func (s *setupRunner) uninstallShells() error {
	if shouldOfferPowerShellSetup() {
		if err := s.runPowerShellShellSetup("", true); err != nil {
			return err
		}
	}
	for _, profile := range posixShellProfiles() {
		if err := s.uninstallPosixProfile(profile); err != nil {
			return err
		}
	}
	fmt.Fprintln(s.out)
	fmt.Fprintln(s.out, "Done. Your ~/.yoconf and the yo binary are untouched.")
	return nil
}

func (s *setupRunner) uninstallShell(target string) error {
	switch target {
	case "bash":
		return s.uninstallBash()
	case "zsh":
		return s.uninstallZsh()
	default:
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot locate the yo binary: %w", err)
		}
		return s.runPowerShellShellSetup(exe, true)
	}
}

func defaultShouldOfferPowerShellSetup() bool {
	return runtime.GOOS == "windows" || powerShellSetupHost() != ""
}

// removeStateFiles is the shared, cross-shell tail of uninstall: it offers to
// delete the state yo created on its own -- the token tally and the session-memory
// cache -- and always leaves ~/.yoconf (your provider + key) untouched, saying so.
// Declining leaves everything in place; uninstall never fails on this step.
func (s *setupRunner) removeStateFiles() {
	s.step("Removing yo's saved state")
	s.info("token usage:   " + tokens.Path())
	s.info("session cache: " + session.Path())
	if s.confirm("Delete these?") {
		if err := tokens.Remove(); err != nil {
			s.warn("could not remove the token usage file: " + err.Error())
		}
		if err := session.Clear(); err != nil {
			s.warn("could not remove the session cache: " + err.Error())
		}
		s.good("removed yo's saved state")
	} else {
		s.warn("left yo's saved state in place")
	}

	// ~/.yoconf is yours (you authored it; it holds your key) -- never auto-delete
	// it, but say plainly that we left it so it isn't a surprise.
	if yoconf, err := yoconfPathFromEnv(os.Getenv); err == nil {
		if _, statErr := os.Stat(yoconf); statErr == nil {
			s.info("left " + yoconf + " untouched (your provider and API key live there);")
			s.info("delete it yourself if you want it gone.")
		}
	}
}

func setupTargetShell() string {
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	if shellIsZsh(os.Getenv("YO_SHELL")) || shellIsZsh(os.Getenv("SHELL")) {
		return "zsh"
	}
	if shellIsBash(os.Getenv("YO_SHELL")) || shellIsBash(os.Getenv("SHELL")) {
		return "bash"
	}
	if runtime.GOOS == "darwin" {
		return "zsh"
	}
	return "powershell"
}

func shellIsZsh(name string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(name)))
	return base == "zsh" || strings.HasSuffix(base, "-zsh")
}

func shellIsBash(name string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(name)))
	return base == "bash" || strings.HasSuffix(base, "-bash")
}

// runPowerShellShellSetup delegates only PowerShell-native work to PowerShell:
// user PATH, PSReadLine, and $PROFILE wiring/removal. Provider/key setup and
// validation are shared Go code below.
func (s *setupRunner) runPowerShellShellSetup(exe string, uninstall bool) error {
	host := powerShellSetupHost()
	if host == "" {
		return fmt.Errorf("setup needs PowerShell (pwsh 7+ or Windows PowerShell 5.1) on PATH")
	}
	tmp, err := os.CreateTemp("", "yo-setup-*.ps1")
	if err != nil {
		return fmt.Errorf("setup error: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(shell.SetupPowerShell); err != nil {
		tmp.Close()
		return fmt.Errorf("setup error: %w", err)
	}
	tmp.Close()

	cmd := exec.Command(host, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", tmp.Name())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, s.out, s.err
	cmd.Env = append(os.Environ(), "YO_SETUP_BIN="+exe)
	if uninstall {
		cmd.Env = append(cmd.Env, "YO_SETUP_UNINSTALL=1")
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("PowerShell setup failed: %w", err)
	}
	return nil
}

func detectPowerShellSetupHost() string {
	// Run setup under the shell that invoked yo, so it configures THAT host's
	// $PROFILE (Windows PowerShell 5.1 or pwsh 7+). Fall back to a PATH lookup.
	if host := parentShell(); host != "" {
		return host
	}
	if p, e := exec.LookPath("pwsh"); e == nil {
		return p
	}
	if p, e := exec.LookPath("powershell"); e == nil {
		return p
	}
	return ""
}

func (s *setupRunner) configureKey() error {
	s.step("Checking for an API key")
	if os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("XAI_API_KEY") != "" || os.Getenv("GEMINI_API_KEY") != "" {
		s.good("an API key is set in your environment")
		return nil
	}
	if cfg, err := config.Load(); err == nil && cfg.Key != "" {
		s.good("an API key is set in ~/.yoconf")
		return nil
	} else if err != nil {
		s.warn("could not read ~/.yoconf yet: " + err.Error())
	}

	s.warn("no ANTHROPIC_API_KEY, OPENAI_API_KEY, XAI_API_KEY, or GEMINI_API_KEY found")
	yoconf, err := yoconfPathFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	s.info("I can store a provider and key in:")
	s.info("    " + yoconf)
	s.info("The standard environment variables still work too; ~/.yoconf is the portable setup path.")
	if !s.confirm("Write provider and key to ~/.yoconf?") {
		s.warn("skipped -- set ANTHROPIC_API_KEY, OPENAI_API_KEY, XAI_API_KEY, or GEMINI_API_KEY when ready")
		return nil
	}

	s.info("Which provider do you want to configure?")
	s.info("    1) Anthropic (Claude)")
	s.info("    2) OpenAI (GPT)")
	s.info("    3) Grok (xAI)")
	s.info("    4) Gemini (Google)")
	provider := parseProviderChoice(s.prompt("Choose [1/2/3/4] (Enter to skip)"))
	if provider == "" {
		s.warn("skipped -- set ANTHROPIC_API_KEY, OPENAI_API_KEY, XAI_API_KEY, or GEMINI_API_KEY when ready")
		return nil
	}

	key := strings.TrimSpace(s.promptSecret("Paste your " + provider + " API key (Enter to skip)"))
	if key == "" {
		s.warn("skipped -- set the API key when ready")
		return nil
	}
	if err := upsertYoconfProviderKey(yoconf, provider, key); err != nil {
		return err
	}
	s.good("wrote provider and key to " + yoconf)
	return nil
}

func (s *setupRunner) validate(target string) {
	s.step("Validating configuration")
	exe, err := os.Executable()
	if err != nil {
		s.warn("could not run yo --check: " + err.Error())
		return
	}
	cmd := exec.Command(exe, "--check")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, s.out, s.err
	cmd.Env = append(os.Environ(), "YO_SHELL="+target)
	if err := cmd.Run(); err != nil {
		s.warn("validation reported a problem; fix it and rerun yo --check")
	}
}

func (s *setupRunner) step(m string) {
	fmt.Fprintln(s.out)
	fmt.Fprintln(s.out, "==> "+m)
}

func (s *setupRunner) good(m string) {
	fmt.Fprintln(s.out, "    OK  "+m)
}

func (s *setupRunner) warn(m string) {
	fmt.Fprintln(s.out, "    !   "+m)
}

func (s *setupRunner) info(m string) {
	fmt.Fprintln(s.out, "    "+m)
}

func (s *setupRunner) confirm(prompt string) bool {
	fmt.Fprint(s.out, "    "+prompt+" [Y/n] ")
	ans, ok := s.readLine()
	if !ok {
		fmt.Fprintln(s.out)
		s.warn("no input available; skipped")
		return false
	}
	switch strings.ToLower(strings.TrimSpace(ans)) {
	case "", "y", "yes":
		return true
	default:
		return false
	}
}

func (s *setupRunner) prompt(prompt string) string {
	fmt.Fprint(s.out, "    "+prompt+" ")
	ans, ok := s.readLine()
	if !ok {
		fmt.Fprintln(s.out)
		return ""
	}
	return strings.TrimSpace(ans)
}

func (s *setupRunner) promptSecret(prompt string) string {
	fmt.Fprint(s.out, "    "+prompt+" ")
	if s.inFile != nil && term.IsTerminal(int(s.inFile.Fd())) {
		b, err := term.ReadPassword(int(s.inFile.Fd()))
		fmt.Fprintln(s.out)
		if err == nil {
			return string(b)
		}
		s.warn("could not read a masked value; falling back to plain input")
	}
	ans, ok := s.readLine()
	if !ok {
		fmt.Fprintln(s.out)
		return ""
	}
	return ans
}

func (s *setupRunner) readLine() (string, bool) {
	line, err := s.in.ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return strings.TrimRight(line, "\r\n"), true
}

func yoconfPathFromEnv(getenv func(string) string) (string, error) {
	home := strings.TrimSpace(getenv("HOME"))
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
	}
	return filepath.Join(home, ".yoconf"), nil
}

func parseProviderChoice(choice string) string {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "1", "anthropic":
		return "anthropic"
	case "2", "openai":
		return "openai"
	case "3", "grok", "xai":
		return "grok"
	case "4", "gemini", "google":
		return "gemini"
	default:
		return ""
	}
}

func upsertYoconfProviderKey(path, provider, key string) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	key = strings.TrimSpace(key)
	if provider != "anthropic" && provider != "openai" && provider != "grok" && provider != "gemini" {
		return fmt.Errorf("provider %q not supported (use \"anthropic\", \"openai\", \"grok\", or \"gemini\")", provider)
	}
	if key == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		lines = strings.Split(text, "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	foundProvider, foundKey := false, false
	for i, line := range lines {
		switch activeYoconfDirective(line) {
		case "provider":
			lines[i] = "provider " + provider
			foundProvider = true
		case "key":
			lines[i] = "key " + key
			foundKey = true
		}
	}
	if !foundProvider || !foundKey {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		if !foundProvider {
			lines = append(lines, "provider "+provider)
		}
		if !foundKey {
			lines = append(lines, "key "+key)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	_ = os.Chmod(path, 0o600)
	return nil
}

func activeYoconfDirective(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return ""
	}
	name, _, ok := strings.Cut(line, " ")
	if !ok {
		name, _, ok = strings.Cut(line, "\t")
	}
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(name))
}
