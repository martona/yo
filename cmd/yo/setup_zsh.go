// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	zshInitLine       = `if command -v yo >/dev/null 2>&1; then eval "$(yo --init zsh)"; fi`
	zshInitMarker     = "yo --init zsh"
	zshManagedStart   = "# >>> yo initialize >>>"
	zshManagedEnd     = "# <<< yo initialize <<<"
	zshLegacyComment  = "# yo - LLM command assistant"
	zshManagedComment = "# Added by `yo --setup`; remove with `yo --uninstall`."
)

func (s *setupRunner) uninstallZsh() error {
	profile, err := zshProfilePathFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	s.step("Removing yo from your zsh profile")
	data, err := os.ReadFile(profile)
	if err != nil {
		if os.IsNotExist(err) {
			s.good("nothing to remove")
			fmt.Fprintln(s.out)
			fmt.Fprintln(s.out, "Done. Your ~/.yoconf and the yo binary are untouched.")
			return nil
		}
		return fmt.Errorf("reading %s: %w", profile, err)
	}
	next, removed := removeZshInit(string(data))
	if !removed {
		s.good("nothing to remove")
		fmt.Fprintln(s.out)
		fmt.Fprintln(s.out, "Done. Your ~/.yoconf and the yo binary are untouched.")
		return nil
	}

	s.info("I can remove the yo integration from:")
	s.info("    " + profile)
	if !s.confirm("Remove it?") {
		s.warn("skipped -- left " + profile + " unchanged")
		fmt.Fprintln(s.out)
		fmt.Fprintln(s.out, "Done. Your ~/.yoconf and the yo binary are untouched.")
		return nil
	}
	if err := os.WriteFile(profile, []byte(next), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", profile, err)
	}
	s.good("removed the integration from " + profile)
	fmt.Fprintln(s.out)
	fmt.Fprintln(s.out, "Done. Your ~/.yoconf and the yo binary are untouched.")
	return nil
}

func (s *setupRunner) checkZshBinaryOnPath(exe string) {
	s.step("Checking the yo binary is available")
	if p, err := exec.LookPath("yo"); err == nil {
		s.good("yo resolves on PATH: " + p)
		return
	}
	s.warn("yo is not on PATH")
	s.info("The managed zsh init block will pin this binary path instead:")
	s.info("    " + exe)
	s.info("For manual setup, put yo on PATH or set YO_BIN to the full path.")
}

func (s *setupRunner) wireZshProfile(exe string) error {
	profile, err := zshProfilePathFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	s.step("Wiring the integration into your zsh profile")
	data, err := os.ReadFile(profile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", profile, err)
	}
	content := string(data)
	if strings.Contains(content, zshInitMarker) {
		s.good("already wired in " + profile)
		return nil
	}

	s.info("I can add the yo init block to your profile:")
	s.info("    " + profile)
	if os.IsNotExist(err) {
		s.info("    (will be created -- it does not exist yet)")
	}
	if !s.confirm("Add it?") {
		s.warn("skipped -- add 'yo --init zsh' to your profile yourself")
		return nil
	}
	next := appendZshInitBlock(content, zshManagedBlock(exe))
	if err := os.MkdirAll(filepath.Dir(profile), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(profile), err)
	}
	if err := os.WriteFile(profile, []byte(next), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", profile, err)
	}
	s.good("added the integration block to " + profile)
	return nil
}

func zshProfilePathFromEnv(getenv func(string) string) (string, error) {
	dir := strings.TrimSpace(getenv("ZDOTDIR"))
	if dir == "" {
		dir = strings.TrimSpace(getenv("HOME"))
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		dir = home
	}
	return filepath.Join(dir, ".zshrc"), nil
}

func zshManagedBlock(exe string) string {
	q := shellQuote(exe)
	return strings.Join([]string{
		zshManagedStart,
		zshManagedComment,
		"if command -v yo >/dev/null 2>&1; then",
		`  eval "$(yo --init zsh)"`,
		"elif [ -x " + q + " ]; then",
		"  export YO_BIN=" + q,
		"  eval \"$(" + q + " --init zsh)\"",
		"fi",
		zshManagedEnd,
	}, "\n")
}

func appendZshInitBlock(content, block string) string {
	if content == "" {
		return block + "\n"
	}
	sep := "\n"
	if strings.HasSuffix(content, "\n\n") {
		sep = ""
	} else if strings.HasSuffix(content, "\n") {
		sep = "\n"
	} else {
		sep = "\n\n"
	}
	return content + sep + block + "\n"
}

func removeZshInit(content string) (string, bool) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	removed := false
	inBlock := false
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if inBlock {
			removed = true
			if trimmed == zshManagedEnd {
				inBlock = false
			}
			continue
		}
		if trimmed == zshManagedStart {
			inBlock = true
			removed = true
			continue
		}
		if trimmed == zshLegacyComment && i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == zshInitLine {
			removed = true
			continue
		}
		if trimmed == zshInitLine {
			removed = true
			continue
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n"), removed
}
