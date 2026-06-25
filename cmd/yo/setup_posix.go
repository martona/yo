// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	shellManagedStart   = "# >>> yo initialize >>>"
	shellManagedEnd     = "# <<< yo initialize <<<"
	shellPathStart      = "# >>> yo path >>>"
	shellPathEnd        = "# <<< yo path <<<"
	shellManagedComment = "# Added by `yo --setup`; remove with `yo --uninstall`."
	shellLegacyComment  = "# yo - LLM command assistant"
)

type posixShellProfile struct {
	Shell       string
	DisplayName string
	ProfilePath func(func(string) string) (string, error)
}

var (
	bashProfile = posixShellProfile{Shell: "bash", DisplayName: "bash", ProfilePath: bashProfilePathFromEnv}
	zshProfile  = posixShellProfile{Shell: "zsh", DisplayName: "zsh", ProfilePath: zshProfilePathFromEnv}
)

func posixShellProfiles() []posixShellProfile {
	return []posixShellProfile{bashProfile, zshProfile}
}

func (s *setupRunner) ensurePosixBinaryOnPath(exe string) (localBin string, needsPath bool, ok bool, err error) {
	s.step("Checking the yo binary is available to POSIX shells")
	if p, err := exec.LookPath("yo"); err == nil {
		s.good("yo resolves on PATH: " + p)
		return "", false, true, nil
	}
	s.warn("yo is not on PATH")
	home, err := posixHomeDir()
	if err != nil {
		return "", false, false, err
	}
	localBin = filepath.Join(home, ".local", "bin")
	target := filepath.Join(localBin, posixInstallName())
	s.info("The managed bash/zsh init blocks use 'command -v yo'.")
	s.info("I can copy this binary:")
	s.info("    " + exe)
	s.info("to:")
	s.info("    " + target)
	if !s.confirm("Copy it there?") {
		s.warn("skipped bash/zsh profile wiring")
		return "", false, false, nil
	}
	if err := copyExecutable(exe, target); err != nil {
		return "", false, false, err
	}
	s.good("copied yo to " + target)
	if pathContainsDir(os.Getenv("PATH"), localBin) {
		return localBin, false, true, nil
	}
	return localBin, true, true, nil
}

func (s *setupRunner) ensureLocalBinOnProfilePath(profile posixShellProfile, localBin string) (bool, error) {
	path, err := profile.ProfilePath(os.Getenv)
	if err != nil {
		return false, err
	}
	s.step("Adding ~/.local/bin to your " + profile.DisplayName + " PATH")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	content := string(data)
	if strings.Contains(content, shellPathStart) || profileAddsLocalBin(content, localBin) {
		s.good("already adds ~/.local/bin in " + path)
		return true, nil
	}
	s.info("I can add a PATH block to your profile:")
	s.info("    " + path)
	if os.IsNotExist(err) {
		s.info("    (will be created -- it does not exist yet)")
	}
	if !s.confirm("Add ~/.local/bin to PATH there?") {
		s.warn("skipped " + profile.DisplayName + " wiring -- yo would not resolve in that shell")
		return false, nil
	}
	next := appendShellInitBlock(content, shellPathBlock())
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	s.good("added ~/.local/bin to " + path)
	return true, nil
}

func (s *setupRunner) wirePosixProfile(profile posixShellProfile, exe string) error {
	path, err := profile.ProfilePath(os.Getenv)
	if err != nil {
		return err
	}
	s.step("Wiring the integration into your " + profile.DisplayName + " profile")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	content := string(data)
	if strings.Contains(content, shellInitMarker(profile.Shell)) {
		s.good("already wired in " + path)
		return nil
	}

	s.info("I can add the yo init block to your profile:")
	s.info("    " + path)
	if os.IsNotExist(err) {
		s.info("    (will be created -- it does not exist yet)")
	}
	if !s.confirm("Add it?") {
		s.warn("skipped -- add 'yo --init " + profile.Shell + "' to your profile yourself")
		return nil
	}
	next := appendShellInitBlock(content, shellManagedBlock(profile.Shell, exe))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	s.good("added the integration block to " + path)
	return nil
}

func (s *setupRunner) uninstallPosixProfile(profile posixShellProfile) error {
	path, err := profile.ProfilePath(os.Getenv)
	if err != nil {
		return err
	}
	s.step("Removing yo from your " + profile.DisplayName + " profile")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.good("nothing to remove")
			return nil
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}
	next, removed := removeShellInit(profile.Shell, string(data))
	if !removed {
		s.good("nothing to remove")
		return nil
	}

	s.info("I can remove the yo integration from:")
	s.info("    " + path)
	if !s.confirm("Remove it?") {
		s.warn("skipped -- left " + path + " unchanged")
		return nil
	}
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	s.good("removed the integration from " + path)
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

func posixHomeDir() (string, error) {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home != "" {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return home, nil
}

func posixInstallName() string {
	if runtime.GOOS == "windows" {
		return "yo.exe"
	}
	return "yo"
}

func copyExecutable(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("reading %s: %w", src, err)
	}
	if dstInfo, err := os.Stat(dst); err == nil && os.SameFile(srcInfo, dstInfo) {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", dst, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dst), err)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening %s: %w", src, err)
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".yo-copy-*")
	if err != nil {
		return fmt.Errorf("creating temporary file in %s: %w", filepath.Dir(dst), err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return fmt.Errorf("copying %s to %s: %w", src, dst, err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return fmt.Errorf("marking %s executable: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("installing %s: %w", dst, err)
	}
	return nil
}

func pathContainsDir(pathValue, dir string) bool {
	dir = filepath.Clean(dir)
	for _, part := range filepath.SplitList(pathValue) {
		if part == "" {
			continue
		}
		if filepath.Clean(part) == dir {
			return true
		}
	}
	return false
}

func profileAddsLocalBin(content, localBin string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "$HOME/.local/bin") ||
			strings.Contains(line, "${HOME}/.local/bin") ||
			strings.Contains(line, "~/.local/bin") ||
			strings.Contains(line, localBin) {
			return true
		}
	}
	return false
}

func shellPathBlock() string {
	return strings.Join([]string{
		shellPathStart,
		shellManagedComment,
		`case ":$PATH:" in`,
		`  *":$HOME/.local/bin:"*) ;;`,
		`  *) export PATH="$HOME/.local/bin:$PATH" ;;`,
		`esac`,
		shellPathEnd,
	}, "\n")
}

func shellInitLine(shellName string) string {
	return `if command -v yo >/dev/null 2>&1; then eval "$(yo --init ` + shellName + `)"; fi`
}

func shellInitMarker(shellName string) string {
	return "yo --init " + shellName
}

func shellManagedBlock(shellName, _ string) string {
	return strings.Join([]string{
		shellManagedStart,
		shellManagedComment,
		"if command -v yo >/dev/null 2>&1; then",
		"  eval \"$(yo --init " + shellName + ")\"",
		"fi",
		shellManagedEnd,
	}, "\n")
}

func appendShellInitBlock(content, block string) string {
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

func removeShellInit(shellName, content string) (string, bool) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	removed := false
	inBlock := false
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if inBlock {
			removed = true
			if trimmed == shellManagedEnd || trimmed == shellPathEnd {
				inBlock = false
			}
			continue
		}
		if trimmed == shellManagedStart || trimmed == shellPathStart {
			inBlock = true
			removed = true
			continue
		}
		if trimmed == shellLegacyComment && i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == shellInitLine(shellName) {
			removed = true
			continue
		}
		if trimmed == shellInitLine(shellName) {
			removed = true
			continue
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n"), removed
}

func (s *setupRunner) wireBashProfile(exe string) error {
	return s.wirePosixProfile(bashProfile, exe)
}

func (s *setupRunner) wireZshProfile(exe string) error {
	return s.wirePosixProfile(zshProfile, exe)
}

func (s *setupRunner) uninstallBash() error {
	return s.uninstallPosixProfile(bashProfile)
}

func (s *setupRunner) uninstallZsh() error {
	return s.uninstallPosixProfile(zshProfile)
}

func bashManagedBlock(exe string) string {
	return shellManagedBlock("bash", exe)
}

func zshManagedBlock(exe string) string {
	return shellManagedBlock("zsh", exe)
}

func appendBashInitBlock(content, block string) string {
	return appendShellInitBlock(content, block)
}

func appendZshInitBlock(content, block string) string {
	return appendShellInitBlock(content, block)
}

func removeBashInit(content string) (string, bool) {
	return removeShellInit("bash", content)
}

func removeZshInit(content string) (string, bool) {
	return removeShellInit("zsh", content)
}
