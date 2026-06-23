# macOS + zsh Port Plan

This plan adds first-class macOS support with zsh as the target shell, while
preserving maximum reuse from the existing Windows + PowerShell implementation.
The intended end state is behavioral parity, not identical internals: the Go core
and wire protocol stay shared, while each shell gets a thin adapter for its own
line editor and lifecycle hooks.

## Goals

- Support `yo <natural language>` from an interactive zsh prompt on macOS.
- Capture raw `yo` queries before zsh parses metacharacters.
- Prefill the model's command onto the editable prompt line; never execute it.
- Preserve chat answers, explanations, wrapping, errors, and debug behavior.
- Preserve multi-step continuation with exit-code feedback.
- Capture the exact command the user actually ran after editing.
- Preserve session memory through `YO_SESSION`.
- Use terminal scrollback where available, with the existing redaction path.
- Add `yo --init zsh`, docs, tests, and darwin build coverage.
- Keep the design friendly to future bash + macOS support.

## Non-Goals

- Do not merge the PowerShell and zsh snippets into one cross-shell script.
- Do not require `jq`, Python, or other runtime dependencies for shell parsing.
- Do not implement a PTY proxy or terminal emulator for native macOS scrollback.
- Do not add bash support in the first zsh port, but avoid choices that make it
  harder later.

## High-Level Shape

The port should split responsibilities like this:

- Go core: providers, prompt profiles, result serialization, session memory,
  continuation state, text wrapping, scrollback capture, redaction.
- Shell adapter: raw-line capture, command invocation, result application,
  editable prefill, continuation hooks, cancellation gestures.

PowerShell remains the existing adapter. zsh becomes the first Unix adapter.
Bash should later reuse the same Go-facing contract but use its own Readline and
prompt hooks.

## Phase 0: zsh Interaction Spike

Before large refactors, manually prove the zsh primitives in a small throwaway
snippet.

Verify:

- `print -r -z -- "$cmd"` gives a clean editable next-prompt prefill.
- A wrapped `accept-line` ZLE widget can inspect and rewrite `$BUFFER` before zsh
  parses the line.
- `preexec` receives the exact edited command line.
- `precmd` can capture the previous command's exit status if it reads `$?`
  immediately.
- Bare Enter and Ctrl-C can cancel an armed continuation without leaving stale
  `YO_STATE`.
- The approach behaves with vi/emacs keymaps, common zsh plugins, and re-sourcing
  the snippet.

If `print -r -z` fails an important case, fall back to a custom ZLE prefill widget
using `BUFFER=...`, `CURSOR=...`, and `zle redisplay`.

### Phase 0 Spike Result (2026-06-23)

Implemented as [`shell/yo-zsh-spike.zsh`](../shell/yo-zsh-spike.zsh). The spike
is a disposable sourceable zsh script that shadows `yo` with a fake
implementation and validates the shell mechanics without calling the LLM.

Validated in a real zsh PTY:

- A wrapped `accept-line` widget rewrites raw `yo <query>` input before zsh
  parses metacharacters. An unquoted query containing parentheses and a pipe
  reached the fake `yo` function as one literal argument.
- `print -r -z -- "$cmd"` queues an editable command on the next prompt.
- `preexec` captures the edited command the user actually ran.
- `precmd` sees the command's exit status, drives the continuation, and can
  queue the next editable command.
- Re-sourcing avoids stacking duplicate hooks/widgets, and uninstall restores
  the original widgets in the test shell.
- Declining a prefilled command via interrupt/no-run clears the armed
  continuation state.

Important finding: use `print -r -z`, not plain `print -z`. Without `-r`, zsh's
`print` builtin interprets backslash escapes before queueing the editable buffer,
so a command containing `\n` can become a real newline in the line editor.

## Phase 1: Add Prompt Profiles

The current prompt layer is explicitly PowerShell/Windows-oriented. Add a
small shell/OS command profile so provider logic stays shared while command
generation becomes shell-aware.

Implemented shape:

```go
type CommandProfile struct {
	Family              string
	Shell               string
	PromptDescription   string
	CommandTool         string
	CommandField        string
	CommandGuidance     string
	PendingFieldExample string
	MultiStepExample    string
	ScriptLimit         string
	OpenAIExamples      []CommandExample
}

type CommandExample struct {
	Request string
	Command string
}
```

Detection order:

- Prefer `YO_SHELL` from the integration snippet, for example `zsh` or
  `powershell`.
- Use `runtime.GOOS` to preserve the Windows default when no shell is known.
- Fall back to `$SHELL` where useful.
- Keep a config override optional; add it only if detection proves ambiguous.

Profile-aware prompt text should cover:

- Command tool description.
- Command field description.
- Pending field examples.
- Anthropic system prompt.
- OpenAI system prompt and worked examples.
- Multi-step investigate-first examples.

The sample split is by command language, not by macOS vs Linux:

- **PowerShell/Windows:** keep the existing cmdlet examples, such as
  `Get-ChildItem`, `Get-PSDrive`, and `Get-Process`.
- **POSIX/Unix:** use portable/common Unix examples, such as `ls -la`, `df -h`,
  `du`, `find`, `grep`, `sed`, `awk`, `ps -ef`, `sort`, and `xargs`.

Avoid macOS-only or Linux-only prompt examples unless a future profile is added
for that narrower target.

### Phase 1 Result (2026-06-23)

Implemented in `internal/llm/profile.go` and threaded through both providers.
`YO_SHELL=pwsh` selects the PowerShell profile. `YO_SHELL=zsh`, `YO_SHELL=bash`,
or a Unix `$SHELL` selects the POSIX/Unix profile. Unknown non-Windows shells
also default to POSIX/Unix. Tests cover profile detection, prompt separation, and
provider request construction.

## Phase 2: Add A Shell-Friendly Result Format

PowerShell can parse JSON with `ConvertFrom-Json`; zsh cannot rely on a built-in
JSON parser. Avoid adding a `jq` dependency.

Keep JSON as the default/public stdout contract, but add an internal shell
format for Unix snippets:

```text
yo --output sh --shell zsh ...
```

It should emit safely quoted shell assignments:

```sh
YO_RESULT_TYPE='command'
YO_RESULT_COMMAND='...'
YO_RESULT_EXPLANATION='...'
YO_RESULT_PENDING='1'
YO_RESULT_STATE='...'
YO_RESULT_PREFILL_SPACE='0'
```

The zsh snippet can capture and `eval` this output only after Go has quoted
every value safely.

Add tests for shell quoting with:

- Single quotes.
- Newlines.
- Semicolons.
- Backticks.
- Dollar expansion.
- Command substitution, including hostile strings like `$(touch /tmp/bad)`.
- Redirections and pipes.
- Ampersands and angle brackets.

This also creates a reusable contract for future bash support.

## Phase 3: Add `shell/yo.zsh`

Embed a zsh integration beside the existing PowerShell integration.

Core functions:

- `_yo_bin`: resolve `$YO_BIN` or `commands[yo]`.
- `_yo_width`: report `${COLUMNS:-80}`.
- `_yo_info` / `_yo_error`: print explanations, chat, and errors with simple
  terminal coloring; respect `NO_COLOR` if convenient.
- `_yo_invoke`: call the binary with `YO_SHELL=zsh`, `--output sh`, and
  `--width`.
- `_yo_apply_result`: handle command/chat/error results, prefill commands, and
  manage continuation state.
- `yo`: user-facing function; cancels any in-progress continuation before
  starting a new query.

### Prefill

Start with:

```zsh
print -r -z -- "$cmd"
```

If `prefill_space` is enabled, prefix the command in the snippet, matching the
PowerShell adapter's behavior.

### Raw-Line Capture

Wrap the current `accept-line` widget:

- Save the existing widget so re-sourcing is safe.
- Inspect `$BUFFER` before parsing.
- Rewrite only when the first token is exactly `yo` followed by a query.
- Skip queries that start with `-`, so debug flags pass through.
- Skip already-quoted single-token queries to make history recall idempotent.
- Quote the query using zsh-safe single-quote escaping.
- Call the original `accept-line` implementation afterward.

This is the zsh equivalent of the PowerShell Enter-key hook.

### Continuation

Use zsh hooks:

```zsh
autoload -Uz add-zsh-hook
add-zsh-hook preexec _yo_preexec
add-zsh-hook precmd _yo_precmd
```

Behavior:

- On pending command, store `YO_STATE` and arm the continuation.
- `preexec` stores the exact edited command line in `YO_RAN`.
- `precmd` captures `$?` immediately, then calls `yo --continue --exit <code>`.
- The binary returns the next command/chat/error through the same result format.
- A terminal command, chat answer, new `yo ...`, bare Enter, Ctrl-C, or step cap
  clears state.

Do not depend on shell history for executed-command capture. zsh users may have
history settings that ignore space-prefixed commands.

### Session Memory

Set `YO_SESSION` once per shell session if missing:

```zsh
export YO_SESSION="${$}-$(openssl rand -hex 4 2>/dev/null || date +%s)"
```

Prefer a dependency-free nonce if possible. The value only needs to avoid obvious
PID reuse collisions.

## Phase 4: Unix Scrollback

Current non-Windows scrollback supports zellij but not tmux. Add tmux capture
early because tmux is common on macOS.

Capture priority:

1. zellij when `$ZELLIJ` is set.
2. tmux when `$TMUX` is set.
3. Windows console fallback on Windows.
4. Empty string elsewhere.

tmux command:

```sh
tmux capture-pane -pS -200
```

Keep the existing redaction path in `withScrollback`, so captured terminal
output is scrubbed before it leaves the machine.

Plain macOS terminal scrollback remains unavailable without a multiplexer. That
is expected and should be documented clearly.

## Phase 5: Init, Setup, And Uninstall

Add:

- `shell.Zsh` embed.
- `yo --init zsh`.
- Help text showing both PowerShell and zsh.
- README instructions for macOS zsh.

Manual init line:

```zsh
if command -v yo >/dev/null 2>&1; then eval "$(yo --init zsh)"; fi
```

For full parity, add a macOS/zsh branch to `yo --setup`:

- Detect profile path: `${ZDOTDIR:-$HOME}/.zshrc`.
- Confirm before editing.
- Add the managed init line idempotently.
- Prompt for provider and API key, or write key guidance to `~/.yoconf`.
- Run `yo --check`.

Add `--uninstall` support for zsh by removing only the managed init block or
managed init line. Keep the PowerShell setup path intact.

## Phase 6: Tests

Go tests:

- Prompt profile selection for PowerShell vs zsh.
- `--dry-run` includes macOS/zsh prompt text when `YO_SHELL=zsh`.
- `--output sh` quoting is injection-safe.
- `--init zsh` emits the zsh snippet.
- tmux capture uses a fake `tmux` on `PATH`.
- Existing PowerShell behavior remains unchanged.

Shell checks:

- `zsh -n shell/yo.zsh`.
- Source the snippet non-interactively with a fake `YO_BIN`.
- Exercise result parsing and state transitions with fake command output.
- Confirm re-sourcing does not stack duplicate hooks/widgets.

Manual interactive checklist:

- `yo what does (echo hi | wc -c) mean`
- Command prefill, edit, run.
- Chat answer.
- Multi-step continuation.
- Edit generated command and verify the continuation sees the edited command.
- Bare Enter cancels.
- Ctrl-C cancels.
- `yo why did that fail` inside tmux or zellij.
- No multiplexer degrades gracefully.
- `prefill_space true` does not break continuation.

## Phase 7: macOS Builds And Docs

Add darwin build legs:

- `darwin/arm64`
- `darwin/amd64`

Update:

- `README.md`
- `yoconf.example`
- `docs/design-notes.md`
- release workflow asset names
- packaging notes for future Homebrew support

Early macOS installation can prefer:

```sh
go install github.com/martona/yo/cmd/yo@latest
```

Downloaded prebuilt binaries may hit Gatekeeper/quarantine friction until
notarization is added. Notarization can be a later release-hardening step.

## Future Bash Compatibility

Do not try to share the zsh shell script with bash. Share the Go-facing contract
instead.

Reusable for bash later:

- Prompt profiles.
- `--output sh`.
- tmux/zellij scrollback.
- `YO_STATE`, `YO_RAN`, and `YO_SESSION`.
- Continuation state machine.
- Result application conventions.

Bash-specific work later:

- Readline `bind -x` instead of ZLE widgets.
- `PROMPT_COMMAND` instead of `precmd`.
- `DEBUG` trap or a preexec emulation instead of native `preexec`.
- Compatibility with macOS's system bash 3.2.
- More care around existing user `PROMPT_COMMAND` and key bindings.

The desired outcome is that bash only needs a new hook adapter, not another
round of core changes.

## Suggested Implementation Order

1. Run the zsh spike and record any surprises.
2. Add prompt profiles while preserving PowerShell output.
3. Add `--output sh` and quoting tests.
4. Add `shell/yo.zsh` with `--init zsh`.
5. Add tmux scrollback.
6. Add zsh setup/uninstall.
7. Add docs and darwin build legs.
8. Run the manual interactive checklist on macOS.
