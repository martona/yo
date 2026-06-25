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
	bashInitLine       = `if command -v yo >/dev/null 2>&1; then eval "$(yo --init bash)"; fi`
	bashInitMarker     = "yo --init bash"
	bashManagedStart   = "# >>> yo initialize >>>"
	bashManagedEnd     = "# <<< yo initialize <<<"
	bashLegacyComment  = "# yo - LLM command assistant"
	bashManagedComment = "# Added by `yo --setup`; remove with `yo --uninstall`."
)

func (s *setupRunner) uninstallBash() error {
	profile, err := bashProfilePathFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	s.step("Removing yo from your bash profile")
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
	next, removed := removeBashInit(string(data))
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

func (s *setupRunner) checkBashBinaryOnPath(exe string) {
	s.step("Checking the yo binary is available")
	if p, err := exec.LookPath("yo"); err == nil {
		s.good("yo resolves on PATH: " + p)
		return
	}
	s.warn("yo is not on PATH")
	s.info("The managed bash init block will pin this binary path instead:")
	s.info("    " + exe)
	s.info("For manual setup, put yo on PATH or set YO_BIN to the full path.")
}

func (s *setupRunner) wireBashProfile(exe string) error {
	profile, err := bashProfilePathFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	s.step("Wiring the integration into your bash profile")
	data, err := os.ReadFile(profile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", profile, err)
	}
	content := string(data)
	if strings.Contains(content, bashInitMarker) {
		s.good("already wired in " + profile)
		return nil
	}

	s.info("I can add the yo init block to your profile:")
	s.info("    " + profile)
	if os.IsNotExist(err) {
		s.info("    (will be created -- it does not exist yet)")
	}
	if !s.confirm("Add it?") {
		s.warn("skipped -- add 'yo --init bash' to your profile yourself")
		return nil
	}
	next := appendBashInitBlock(content, bashManagedBlock(exe))
	if err := os.MkdirAll(filepath.Dir(profile), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(profile), err)
	}
	if err := os.WriteFile(profile, []byte(next), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", profile, err)
	}
	s.good("added the integration block to " + profile)
	return nil
}

func bashProfilePathFromEnv(getenv func(string) string) (string, error) {
	home := strings.TrimSpace(getenv("HOME"))
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
	}
	return filepath.Join(home, ".bashrc"), nil
}

func bashManagedBlock(exe string) string {
	q := shellQuote(exe)
	return strings.Join([]string{
		bashManagedStart,
		bashManagedComment,
		"if command -v yo >/dev/null 2>&1; then",
		`  eval "$(yo --init bash)"`,
		"elif [ -x " + q + " ]; then",
		"  export YO_BIN=" + q,
		"  eval \"$(" + q + " --init bash)\"",
		"fi",
		bashManagedEnd,
	}, "\n")
}

func appendBashInitBlock(content, block string) string {
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

func removeBashInit(content string) (string, bool) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	removed := false
	inBlock := false
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if inBlock {
			removed = true
			if trimmed == bashManagedEnd {
				inBlock = false
			}
			continue
		}
		if trimmed == bashManagedStart {
			inBlock = true
			removed = true
			continue
		}
		if trimmed == bashLegacyComment && i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == bashInitLine {
			removed = true
			continue
		}
		if trimmed == bashInitLine {
			removed = true
			continue
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n"), removed
}
