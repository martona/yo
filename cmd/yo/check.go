// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/martona/yo/internal/config"
)

type checkResult struct {
	Status string
	Text   string
}

// runCheck validates local configuration and prints advisory shell diagnostics.
// Config/key problems keep the historical non-zero exit; shell integration issues
// are reported but do not fail the check because users often have unused shells.
func runCheck() {
	configOK := true

	fmt.Println("yo check")
	fmt.Println()
	fmt.Println("binary:")
	for _, r := range binaryCheckResults() {
		printCheckResult(r)
	}

	fmt.Println()
	fmt.Println("config:")
	cfg, err := config.Load()
	if err != nil {
		configOK = false
		printCheckResult(checkResult{"ERR", "config: " + err.Error()})
	} else if err := cfg.Ready(); err != nil {
		configOK = false
		printCheckResult(checkResult{"ERR", "key: " + err.Error()})
	} else {
		mem := "off"
		if cfg.Memory {
			mem = "on"
		}
		dbgState := "off"
		if cfg.Debug {
			dbgState = "on"
		}
		printCheckResult(checkResult{"OK", fmt.Sprintf("provider=%s  model=%s  memory=%s  debug=%s  key=%d chars (decoded & valid)", cfg.Provider, cfg.Model, mem, dbgState, len(cfg.Key))})
	}

	fmt.Println()
	fmt.Println("shells:")
	for _, r := range shellCheckResults() {
		printCheckResult(r)
	}

	if !configOK {
		os.Exit(1)
	}
}

func printCheckResult(r checkResult) {
	fmt.Printf("%-4s %s\n", r.Status, r.Text)
}

func binaryCheckResults() []checkResult {
	var out []checkResult
	if exe, err := os.Executable(); err == nil {
		out = append(out, checkResult{"OK", "executable: " + exe})
	} else {
		out = append(out, checkResult{"WARN", "executable: " + err.Error()})
	}
	if p, err := exec.LookPath("yo"); err == nil {
		out = append(out, checkResult{"OK", "PATH: yo -> " + p})
	} else {
		out = append(out, checkResult{"WARN", "PATH: yo not found; shell init blocks use 'command -v yo'"})
	}
	return out
}

func shellCheckResults() []checkResult {
	return []checkResult{
		powerShellCheckResult(),
		zshCheckResult(),
		bashCheckResult(),
	}
}

func powerShellCheckResult() checkResult {
	host := powerShellSetupHost()
	if host == "" {
		return checkResult{"INFO", "PowerShell: not found on PATH"}
	}
	info, err := inspectPowerShell(host)
	if err != nil {
		return checkResult{"WARN", "PowerShell: " + host + " found, but diagnostics failed: " + err.Error()}
	}

	parts := []string{host}
	if info["version"] != "" {
		parts = append(parts, "version "+info["version"])
	}
	if psrl := info["psreadline"]; psrl != "" {
		status := "PSReadLine " + psrl
		if !versionAtLeast(psrl, 2, 1) {
			status += " (2.1+ recommended)"
		}
		parts = append(parts, status)
	} else {
		parts = append(parts, "PSReadLine not found")
	}
	if runtime.GOOS == "windows" {
		if policy := info["policy"]; policy != "" {
			parts = append(parts, "execution policy "+policy)
		}
	}
	parts = append(parts, profileWiringStatus(info["profile"], "yo --init powershell"))

	status := "OK"
	if info["psreadline"] == "" || (info["psreadline"] != "" && !versionAtLeast(info["psreadline"], 2, 1)) ||
		profileNeedsAttention(info["profile"], "yo --init powershell") ||
		powerShellPolicyBlocksProfiles(info["policy"]) {
		status = "WARN"
	}
	return checkResult{status, "PowerShell: " + strings.Join(parts, "; ")}
}

func inspectPowerShell(host string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	script := strings.Join([]string{
		"$ErrorActionPreference = 'SilentlyContinue'",
		"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8",
		"$m = Get-Module PSReadLine -ListAvailable | Sort-Object Version -Descending | Select-Object -First 1",
		"$psrl = ''; if ($m) { $psrl = $m.Version.ToString() }",
		"$cu = Get-ExecutionPolicy -Scope CurrentUser",
		"$lm = Get-ExecutionPolicy -Scope LocalMachine",
		"$pol = if ($cu -ne 'Undefined') { $cu } else { $lm }",
		"Write-Output ('version=' + $PSVersionTable.PSVersion.ToString())",
		"Write-Output ('profile=' + $PROFILE)",
		"Write-Output ('psreadline=' + $psrl)",
		"Write-Output ('policy=' + $pol)",
	}, "; ")
	cmd := exec.CommandContext(ctx, host, "-NoProfile", "-Command", script)
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return parseKeyValueLines(decodeShellOutput(out)), nil
}

func zshCheckResult() checkResult {
	path, err := exec.LookPath("zsh")
	if err != nil {
		return checkResult{"INFO", "zsh: not found on PATH"}
	}
	version := shellVersion(path, "-c", "print -r -- $ZSH_VERSION")
	profile, profileErr := zshProfilePathFromEnv(os.Getenv)
	if profileErr != nil {
		return checkResult{"WARN", "zsh: " + path + "; " + profileErr.Error()}
	}
	text := strings.Join([]string{path, version, profileWiringStatus(profile, shellInitMarker("zsh"))}, "; ")
	status := "OK"
	if profileNeedsAttention(profile, shellInitMarker("zsh")) {
		status = "WARN"
	}
	return checkResult{status, "zsh: " + text}
}

func bashCheckResult() checkResult {
	path, err := exec.LookPath("bash")
	if err != nil {
		return checkResult{"INFO", "bash: not found on PATH"}
	}
	version := shellVersion(path, "-c", `printf '%s.%s.%s\n' "${BASH_VERSINFO[0]}" "${BASH_VERSINFO[1]}" "${BASH_VERSINFO[2]}"`)
	profile, profileErr := bashProfilePathFromEnv(os.Getenv)
	if profileErr != nil {
		return checkResult{"WARN", "bash: " + path + "; " + profileErr.Error()}
	}
	parts := []string{path, version}
	status := "OK"
	if !bashVersionSupported(version) {
		status = "WARN"
		parts = append(parts, "integration will no-op (needs bash 4.2+)")
	} else if profileNeedsAttention(profile, shellInitMarker("bash")) {
		status = "WARN"
	}
	parts = append(parts, profileWiringStatus(profile, shellInitMarker("bash")))
	return checkResult{status, "bash: " + strings.Join(parts, "; ")}
}

func shellVersion(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil || ctx.Err() != nil {
		return "version unknown"
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "version unknown"
	}
	return "version " + v
}

func profileWiringStatus(path, marker string) string {
	if path == "" {
		return "profile unknown"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "profile " + path + " not found"
		}
		return "profile " + path + " unreadable: " + err.Error()
	}
	if strings.Contains(string(data), marker) {
		return "profile " + path + " wired"
	}
	return "profile " + path + " not wired"
}

func profileNeedsAttention(path, marker string) bool {
	if path == "" {
		return true
	}
	data, err := os.ReadFile(path)
	return err != nil || !strings.Contains(string(data), marker)
}

func bashVersionSupported(version string) bool {
	nums := versionNumbers(version)
	if len(nums) < 2 {
		return false
	}
	return nums[0] > 4 || (nums[0] == 4 && nums[1] >= 2)
}

func versionAtLeast(version string, major, minor int) bool {
	nums := versionNumbers(version)
	if len(nums) < 2 {
		return false
	}
	return nums[0] > major || (nums[0] == major && nums[1] >= minor)
}

func versionNumbers(s string) []int {
	fields := strings.Fields(s)
	if len(fields) > 0 && strings.EqualFold(fields[0], "version") {
		s = strings.Join(fields[1:], ".")
	}
	var nums []int
	for _, part := range strings.Split(s, ".") {
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(readLeadingDigits(part))
		if err != nil {
			break
		}
		nums = append(nums, n)
	}
	return nums
}

func readLeadingDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

func parseKeyValueLines(s string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			out[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return out
}

func decodeShellOutput(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		return decodeUTF16ForCheck(b[2:], binary.LittleEndian)
	}
	if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		return decodeUTF16ForCheck(b[2:], binary.BigEndian)
	}
	if len(b) >= 2 && len(b)%2 == 0 && strings.Count(string(b), "\x00") >= len(b)/4 {
		return decodeUTF16ForCheck(b, binary.LittleEndian)
	}
	return string(b)
}

func decodeUTF16ForCheck(b []byte, order binary.ByteOrder) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = order.Uint16(b[i*2:])
	}
	return string(utf16.Decode(u))
}

func powerShellPolicyBlocksProfiles(policy string) bool {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "", "undefined", "remotesigned", "unrestricted", "bypass":
		return false
	default:
		return true
	}
}
