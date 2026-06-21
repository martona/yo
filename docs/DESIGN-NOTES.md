# `yo` — Design Notes

A standalone, cross-platform LLM-enabled command assistant. Type `yo <natural language>` at your shell prompt; the request goes to an LLM API, a structured response comes back, and the relevant command is prefilled onto your shell's line editor — ready to run, edit, or cancel. Derived from [yoshell](https://github.com/pizlonator/yosh): it reuses yoshell's prompt design and response protocol directly, but takes its own architectural road for delivery — a standalone native binary instead of a shell fork.

`yo` is an acknowledged **derivative work** of yoshell, licensed **GPLv3** to match. It inherits the command name, the system prompts, and the response/continuation protocol from yoshell's `yo.c`; what it does *not* carry is the Bash/Readline C the original is built on. See [License & provenance](#license--provenance).

---

## Status (2026-06-20)

**v0.1 shipped and works on Windows + PowerShell 7+.** `yo <text>` calls the LLM and prefills a PowerShell command on the next prompt (or prints a chat answer); live-verified on both Anthropic and OpenAI, including multi-step sequences.

- **Layout:** `cmd/yo/` (entry) + `internal/config` + `internal/llm` (a `Provider` interface with `anthropic.go` and `openai.go`). Build: `go build ./cmd/yo`. Tests live beside the code; `go test ./...` is green.
- **Providers:** `anthropic` (Messages API) and `openai` (Responses API), selected via `provider` in `~/.yoconf` or inferred from which key env var (`ANTHROPIC_API_KEY` / `OPENAI_API_KEY`) is set. See [`yoconf.example`](../yoconf.example).
- **Shell integration:** `shell/yo.ps1` — the `yo` function, a one-shot OnIdle next-prompt prefill, and a wrapped `prompt` that drives multi-step. `.ps1` files are kept pure ASCII (PowerShell 5.1 chokes on smart punctuation), and the snippet forces `[Console]::OutputEncoding = UTF-8` so non-ASCII replies render. Key/config files are decoded tolerantly (UTF-8 / UTF-8-BOM / UTF-16), a Windows footgun.
- **Raw-line capture:** a PSReadLine Enter hook single-quotes a `yo <query>` line before PowerShell parses it, so a question can contain `( ) < > & ; | $` without manual quoting; debug-flag calls (`yo --dry-run …`), already-quoted lines, and all non-`yo` input pass through untouched. Requires PSReadLine (pwsh 7+) with a guarded fallback. New — pending live verification on a pwsh 7+ console. See [Raw-line capture (PowerShell)](#raw-line-capture-powershell).
- **Multi-step:** a `pending` command arms a continuation; after you run it, the next step is fetched (with its exit code) and prefilled — repeating until `pending:false`. Built and live-verified. See [Multi-step continuation (as built)](#multi-step-continuation-as-built).
- **Known limitation:** on **Windows PowerShell 5.1**, the bundled PSReadLine 2.0 garbles the programmatic prefill (the command renders doubled) — confirmed not a double-fire on our side (a one-shot handler didn't fix it), so it's a 2.0 render bug. pwsh 7+ is clean, and **5.1 is supported too** once PSReadLine is 2.1+ — `yo --setup` upgrades it (`-Scope CurrentUser`, no admin). So pwsh 7+ is recommended (smoother), not required.
- **Scrollback:** inside **zellij**, recent screen output is captured and folded into the query as context ("why did that fail?") — `zellij action dump-screen --full --path <file>`, ANSI-stripped, last 200 lines, then secrets scrubbed before send (`internal/scrollback` + `internal/redact`). Outside zellij: a no-op on Unix, but on Windows it falls back to the console buffer (viewport under Windows Terminal, full buffer under conhost).
- **Output-fed continuation:** **done** — continuation steps now fold in scrollback the same way the initial query does (in zellij), so the model reacts to each step's real output, not just its exit code. **Secret redaction:** **done** too — outbound scrollback is scrubbed by gitleaks' engine (imported in-process, embedded ruleset) before send, on both paths (`internal/redact`). **Cross-call session memory:** **done** too — a per-session history of exchanges is folded into later queries (`internal/session`). **Built next:** an install/setup helper + README.

---

## Core principle

The tool is **invisible and zero-cost until explicitly summoned.** No LLM is involved unless you type `yo`. It is never ambient, never intercepting, never in the keypath until called. This is lifted from yoshell's "no LLM involved unless you type `yo`" restraint — it's what makes the tool a scalpel rather than Clippy.

---

## Architecture decision: native binary, not shell scripts

Decision: **a single native binary, cross-compiled to macOS / Linux / Windows.** Not a `.sh` + `.ps1` script pair.

### Why native wins

The app's real work is: HTTPS calls with auth headers, JSON parsing, session memory across invocations, shelling out to terminal multiplexers and post-processing their output, and (later) entropy-based secret filtering. In shell languages every one of these is a fight — JSON means depending on `jq`, HTTPS means string-building `curl` headers, memory means inventing a tempfile serialization scheme.

The decisive factor is **code sharing**, not convenience. A `.sh`/`.ps1` split is not two thin wrappers around shared logic — it is **two complete reimplementations** of the entire brain (API client, JSON handling, prompt assembly, response parsing, multiplexer capture, secret filtering), in two languages that share nothing and fail differently. The script approach has the same surface area as native, just duplicated across two hostile runtimes.

A native binary inverts this: the entire brain is written **once** and cross-compiled. The only per-platform, per-shell code left is the tiny prefill integration snippet — small precisely because it's the only thing that legitimately must be shell-specific.

### The cost of native

Native moves the cost from *duplicated runtime logic* (unbounded, ongoing) to *one-time build/distribution plumbing* (finite, solved): cross-compilation, three binary sets, macOS Gatekeeper signing, Windows SmartScreen flagging of unsigned exes, and an install story (`go install`, release downloads, `winget`/`scoop` on Windows, `brew` on macOS). All well-trodden — Atuin solves the identical set. The finite problem is the better one to own.

### Language: Go

Decision: **Go.** A single statically-linked binary with trivial cross-compilation (`GOOS=windows/darwin/linux`; a pure-Go build needs no C toolchain), a standard library that covers the whole brain (`net/http`, `crypto/tls`, `encoding/json` — none of the `jq`/`curl` fights a shell would impose), fast compiles, and garbage-collected memory safety that recovers the spirit of yoshell's Fil-C build at zero cost. Atuin (a native binary doing exactly this kind of per-shell integration) is the architectural proof point — we copy its *patterns*, not its language.

---

## Architecture decision: what we take from yoshell

**Take the brain, not the body. Keep the command name.**

yoshell is a ~50k-line fork of GNU Bash 5.2.32 + Readline 8.2.13. Split its value in two:

*The brain — taken directly.* The prompt design, the tool/response schema, and the multi-step continuation protocol (all in `readline-8.2.13/yo.c`) are the hard-won, provider-agnostic craft worth having — and they are known to work. We reuse them **directly, including the prompt text itself**; the full breakdown is in [Resolved: prompt handling](#resolved-prompt-handling-studied-from-yoshell). 

*The body — designed around, not carried.* The delivery mechanism is bash-fork-specific, and there's no reason to drag 50k lines of shell internals into a standalone tool. Each piece has a lighter standalone equivalent:

| yoshell mechanism (the body) | Our equivalent |
| --- | --- |
| Readline prefill (required the bash fork) | Per-shell integration snippets |
| PTY-proxy scrollback capture | tmux / zellij screen capture (opt-in) |
| Fil-C memory-safe build | Go (garbage-collected, memory-safe) |

The base system prompt is injected in `bash-5.2.32/bashline.c`; the rest lives in `yo.c`. The single biggest finding from reading it: **yoshell does not parse a JSON blob out of text at all — it uses native tool/function calling.** That reframes most of the prompt-handling questions and is promoted to its own decision below.

---

## License & provenance

`yo` is an acknowledged **derivative work of yoshell**, licensed **GPLv3** to match. 

The reasoning:

- The most valuable thing we take is yoshell's **prompt design and response protocol** — the system prompts, the tool/response schema, the continuation logic — earned by attrition and *known to work*. We reuse it directly, prompt text included. Rewriting those prompts purely to dodge a license would be worse engineering and a pretense of independence we don't have.
- Reusing that copyrightable expression makes `yo` a derivative work regardless of the rewrite into Go. GPLv3 is the honest label — and it is already yoshell's license (and Bash's, and Readline's).
- This is a personal, **non-commercial** project, so the copyleft obligations cost nothing we care about.

What we do **not** inherit is yoshell's Bash/Readline C: `yo` is a fresh native binary, and the only thing crossing over is the `yo.c` design and prompt text. "Derivative work," not "fork."

GPLv3 obligations we take on (all routine):

- Ship the full GPLv3 license text in the repo (`LICENSE`).
- Attribute yoshell (`pizlonator/yosh`) and state plainly that `yo` is a derivative reimplementation of its `yo` feature.
- State the significant changes from the original: standalone native binary (Go); per-shell integration snippets instead of a Readline fork; tmux/zellij scrollback instead of the PTY proxy; the provider set in [v1 scope](#v1-scope).
- Keep `yo`'s own source available under GPLv3.

The `yo.c` line references throughout the resolved section double as provenance for the reused prompt text.

---

## Architecture decision: tool/function calling, not a parsed text contract

Decision: **the model never returns a JSON object we parse out of prose. It calls one of a small set of typed tools, and we force it to** (Anthropic `tool_choice: {"type":"any"}`; OpenAI Responses `tool_choice: "required"`). 

- **"Mode" is the identity of the tool the model chose**, not a self-reported field. `command` vs. `chat` vs. `scrollback` vs. `docs`. Routing is a tool-name→handler switch, with nothing to misparse.
- **The command arrives as a typed argument** (`input.command`), already extracted and schema-validated by the API. The entire class of "wrapped in backticks / appended an explanation / hedged with *you could try…*" failures cannot occur, because the model is filling a schema slot, not writing a string we descrape.
- **Forcing tool use** removes the other half: the model cannot leak free-form prose onto your command line, because it is not permitted to answer except through a tool.

This is fully available to a standalone binary — `tools` + `tool_choice` are just fields in the request body, identical from Go as from a forked shell. We adopt it wholesale. Detailed schemas, the per-provider prompt asymmetry, and the residual failure modes yoshell still had to patch are in [Resolved: prompt handling](#resolved-prompt-handling-studied-from-yoshell).

---

## How it works

### The flow

1. User types `yo find all files larger than 100MB` (or a question).
2. The shell integration snippet captures the argument, invokes the `yo` binary.
3. The binary assembles the request — system prompt + user text + (optional) scrollback context + session memory — and calls the LLM API.
4. A structured JSON response comes back and is parsed.
5. The relevant command is handed back to the shell, which prefills it onto the line editor.
6. User presses Enter to run, edits first, or cancels (Ctrl-C / empty line).

### The prefill is NOT standalone — this is the key correction

Atuin for example installs a per-shell integration snippet (sourced in `.bashrc` / `.zshrc` / PowerShell profile) that binds a key to: run the `atuin` binary, capture its stdout, and let **the shell's own line editor** place the result. The prefill is done *by the shell*, via the integration hook — the binary just prints a string.

Consequences:

- **The binary is standalone; the prefill step is not.** We ship a binary plus a set of init snippets, one per shell. "No readline integration" isn't quite the win it sounds like — we've moved integration from *forking readline* to *scripting each shell's existing line editor*. Far smaller, well-trodden, but not zero, and it scales by **shell count, not OS count.**
- **GNU Readline the library is irrelevant to us.** We don't link or drive libreadline. We drive whatever line editor the user's shell uses, through that shell's scripting interface. Bash uses readline; zsh uses ZLE; PowerShell uses PSReadLine. Think "prefill via shell integration," not "prefill via readline" (the latter would lock us to bash).

Per-shell prefill mechanism (v1 targets):

| Shell | Line editor | Prefill mechanism |
| --- | --- | --- |
| bash | readline | `READLINE_LINE` / `READLINE_POINT` |
| zsh | ZLE | `BUFFER=...; zle redisplay` |
| PowerShell | PSReadLine | `[Microsoft.PowerShell.PSConsoleReadLine]::Insert()` |

(fish `commandline -r`, nushell, elvish are post-v1 nice-to-haves.)

Continuation (multi-step) rides the **same** integration surface and ports almost 1:1 from yoshell's in-readline loop. Beyond prefill, each line editor also exposes a *pre-prompt/startup hook* (to fire the next-step call when the prompt redraws) and a way to *capture the just-executed command* — the only two extra primitives the loop needs, and all three shells have them. The per-shell mapping and the one nuance (recovering the exact post-edit command) are in [Resolved: prompt handling](#resolved-prompt-handling-studied-from-yoshell), Q4.

### Raw-line capture (PowerShell)

By default `yo <text>` is a real PowerShell command line, so a question containing `( ) < > & ; | $` (and friends) would be mangled — or rejected — by the parser before the binary ever saw it. We lift that quoting burden with a **PSReadLine Enter-key hook** ([`shell/yo.ps1`](../shell/yo.ps1), bottom) that captures the raw edit buffer *before* PowerShell parses it and rewrites a `yo` line into a single safely-quoted argument. It is the direct analog of yoshell's `rl_yo_accept_line`, which grabs the raw Readline buffer at accept time (pre-parse). Accept-time is the **only** correct interception point: `;` `|` `<` `>` take effect at parse time, so reading the line *inside* the `yo` function (e.g. `$MyInvocation.Line`) is both too late and unsafe — `yo a; rm b` would already have run `rm b` as a second statement.

**Algorithm, on every Enter press:**

1. Read the raw buffer (`GetBufferState`) — no parsing has happened yet.
2. Rewrite the buffer to `yo '<query>'` **only when all three hold**:
   - the first token is exactly `yo` followed by a query (`^\s*yo\s+…`);
   - the query does **not** start with `-` — so `yo --dry-run …` and other debug-flag calls pass straight through to normal argument parsing;
   - the query is **not already** one well-formed single-quoted token — so a line recalled from history is left alone. The hook is **idempotent**.

   The quoting is `'` + `query.Replace("'", "''")` + `'`. Inside a PowerShell single-quoted string nothing expands and the only escape is `''` → `'`, so this turns *any* text into one literal argument: `| ; & < >` are literal, `$x` / `$(…)` / `@(…)` do not expand, backtick is literal, `"` is literal. The query reaches the binary byte-for-byte, internal whitespace included.
3. **Always** submit via `ValidateAndAcceptLine` (the stock Enter binding). Every non-`yo` line is therefore untouched and behaves exactly as before — including multi-line / incomplete-input handling (open brace → newline, not accept), which `ValidateAndAcceptLine` performs itself. A rewritten `yo` line is always balanced, so it accepts cleanly even when the user typed unbalanced `(` or quotes in the question.

**History** stores the rewritten, quoted form (`yo 'what does (x|y) do'`), which is re-runnable; on re-accept the idempotency guard (step 2, third bullet) sees it is already quoted and passes it through unchanged.

**Caveats — the shakier part, stated plainly:**

- It **takes over the Enter key globally.** Source `yo.ps1` *after* any module that also rebinds Enter (last writer wins). The rewrite is wrapped in `try/catch` and the accept has its own fallback, so a failure degrades to a plain accept — it can never leave the Enter key stuck.
- Requires **PSReadLine 2.x (pwsh 7+)** — the same dependency the prefill already has. If PSReadLine is absent the hook is skipped (with a one-line notice) and `yo` falls back to plain argument parsing: you quote metacharacters yourself.
- A query that **genuinely starts with `-`** (e.g. `yo -h means what?`) is indistinguishable from a debug-flag call and passes through unquoted; quote it in that rare case. A future `yo :: <flags>` sentinel could separate the two cleanly.

### Scrollback context — opt-in, via multiplexer

If you want the model to see what's on your screen ("why did that command fail?"), you opt in by running inside a terminal multiplexer:

- **tmux:** `tmux capture-pane -pS -1000`
- **zellij:** `zellij action dump-screen --full --path <file>` (cross-platform)

This is elegant because both commands return the **already-resolved screen state** — cells, not the raw byte stream. The multiplexer is a real terminal emulator: it ran the state machine and collapsed all the redraw frames (Docker progress, cargo's live block, npm spinners, Atuin's repaints) into the one frame you actually saw. **All the transcript-pollution problems evaporate** because we never see the animation that produced the screen, only the screen.

Design choices:
- Scrollback is **strictly optional, not a cornerstone.** With a multiplexer present → rich clean context on all three platforms. Without one → fall back to no-scrollback (just the `yo` prompt + session memory), which works everywhere.
- This is what keeps Windows from being second-class: the feature that's genuinely hard on native Windows (tmux is WSL-only there; zellij runs but is less battle-tested) is exactly the one we made optional.
- v1 implementation: shell out. Detect `$TMUX` / `$ZELLIJ`, run the capture command, read stdout. (Control protocols are more robust but more work — defer.)
- On **Windows** (our first target) tmux isn't available; the native scrollback source is the Win32 console buffer (`ReadConsoleOutputCharacterW`) — now **built** as the non-zellij fallback (see [Scrollback sources](#scrollback-sources-and-the-no-multiplexer-question)). `Start-Transcript` was rejected (it records the linear byte stream, not the resolved screen).
- Capture depth (`-1000`) configurable, probably default smaller.

**As built (zellij, extended-prompt):** v0.1 ships exactly this, zellij-only (the lone multiplexer candidate on Windows). The **binary** captures it — it inherits `$env:ZELLIJ` from the shell, runs `zellij action dump-screen --full --path <file>`, strips ANSI, and keeps the last 200 lines (`internal/scrollback`). Rather than a model-requested `scrollback` tool (yoshell's approach), it folds the capture into the request **up front** as a framed context block (`llm.WithTerminalContext`) — simpler, no extra round-trip; the framing tells the model the output is past/completed and to use it only if relevant. Applied to both the initial `yo <text>` query and each continuation step (the `--continue` call), so a multi-step task reacts to real output and not just exit codes. The captured text is secret-redacted before send (see [Secret redaction](#secret-redaction)). Outside zellij it falls back to the Windows console buffer (`console_windows.go`); it is a clean no-op only on Unix without a multiplexer.

### Secret redaction

**Built.** Outbound terminal scrollback is scrubbed before it crosses the wire, on the *read* path — `internal/redact`, applied in `withScrollback` so it covers both the initial query and continuation steps. Detection is **gitleaks' engine**, imported as a Go library and run with its full default ruleset, which is *embedded* in our binary (`config.DefaultConfig`) so nothing ships on disk. Redaction itself is just replacing each found secret with `[REDACTED:<rule-id>]`; when any are found the binary prints a durable one-line `yo: redacted N secrets (kinds…)` to stderr (stdout stays the JSON contract). It **fails closed** — if the detector can't be built, the scrollback is dropped rather than sent raw — and is a no-op when nothing was captured (no multiplexer and, on Windows, no console output), so the cost (~12 ms to build the detector + ~9 ms to scan 200 lines) is paid only when there is output to scan.

**Why gitleaks, not a hand-rolled regex set:** detection is the hard, evolving part (per-rule entropy, keyword gating, allowlists, decoders); redaction once a secret is found is trivial; and a curated regex subset would be lossy by construction. The price is a real dependency tree (~200 modules, ~+6 MB binary) — accepted, because it buys a maintained, precision-tuned detector while keeping the single-binary install (users install nothing extra; the gitleaks-*binary* and Docker options were rejected for an interactive tool — process-spawn latency, daemon/silent-fail, no single binary). gitleaks is MIT, which is GPLv3-compatible; attributed in `NOTICE`.

Deliberately **defense-in-depth, not perfection**: gitleaks errs toward precision (it won't flag a lone AWS access-key id with no secret beside it), so a miss is possible — but that's the same exposure that predated redaction, whereas over-redaction (mangling the context we send the model) is the worse failure, so the precision lean is intentional.

Future tiers (deferred): redact the user's query and the continuation command history (not just scrollback); env-var hygiene (whitelist, never serialize all of `env`); surface the exact post-redaction payload (`--dry-run` already prints the assembled request, now with redaction applied); and entropy / local-model stages if ever needed.

### Session memory (as built)

Cross-call continuity: independent `yo` invocations in the same shell session share a small history of prior exchanges, so "delete the top one" after "find the biggest logs" resolves. It is the durable companion to scrollback — scrollback is the raw screen (output, ephemeral, multiplexer-only), memory is the structured intent thread (no output), available everywhere, which is what matters for the non-zellij majority.

**Storage.** A per-session JSON file under the OS temp dir (`internal/session`), keyed by a session id the snippet mints once (`$env:YO_SESSION = "<pid>-<nonce>"`; the nonce dodges PID reuse). Capped to the last 12 exchanges (plus a char budget); stale files are swept (>7 days) on a session's first write. This is yo's **first on-disk footprint** — continuation rode env vars, redaction is stateless. Default on; `memory false` in `~/.yoconf` or an empty `$env:YO_SESSION` disables it.

**Record.** Per exchange: the raw query, plus either the chat answer or the command steps (offered + executed + exit). A multi-step task gets the full thread for free from the continuation `State` at chain end; a standalone command stores the *offered* command only (no follow-up call to learn what actually ran — a deferred enrichment). Written at terminal responses (chat / non-pending command) and at chain end; pending steps aren't written (they live in `$env:YO_STATE`).

**Replay.** On the *initial* query only, a compact `[recent yo history]` block is folded in via `llm.WithSessionMemory`, framed as background-for-continuity and applied *after* scrollback so the two nest to a single `[request]`. `--continue` steps don't re-read it (they carry their own chain).

**Not redacted, deliberately.** Everything stored has already gone to the LLM — your query; the model's own offered command/chat; for multi-step the executed command, which already rides offered-vs-executed — so memory adds no new LLM exposure, only disk persistence, bounded to an ephemeral per-session temp file with no command *output*. This holds *because* storage is temp-only and output-free; moving to persistent storage or storing output would put real secrets on disk and must revisit this.

---

## v1 scope

- **Targets:** native binary on Windows / macOS / Linux — **Windows first** (see [implementation plan](#implementation-plan)).
- **Shells:** **PowerShell first**, then bash + zsh.
- **Providers:** **Anthropic first**, then OpenAI; API keys via the standard `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` env vars (with an optional `key` override in `~/.yoconf`). Key files (yoshell's `~/.anthropickey`) are deliberately not supported — env vars are the expected path.
- **Core loop:** `yo <text>` → API → parse → prefill.
- **Session memory:** per-session conversation context with a token budget — but persisted *across invocations* in a per-session state file, since the binary is short-lived (unlike yoshell's in-process history). Stateless in the first runnable version.
- **Scrollback:** opt-in; tmux/zellij where present, a Windows-native source (transcript / console buffer) on Windows; graceful no-op fallback.
- **Secret redaction:** built (post-v1) — gitleaks' engine (embedded ruleset) scrubs outbound scrollback on the read path; see [Secret redaction](#secret-redaction).

---

## Resolved: prompt handling (studied from yoshell)

All four original open questions are answered. The unifying answer is the [tool-calling decision](#architecture-decision-toolfunction-calling-not-a-parsed-text-contract) above: routing, extraction, and mode-signalling all fall out of native function calling, so what was a "design the contract" problem becomes a "wire up the tools" problem. Line references below are into yoshell's tree (`readline-8.2.13/yo.c` unless noted) as of the studied checkout. Since we reuse these prompts and contracts directly (GPLv3 — see [License & provenance](#license--provenance)), the references double as provenance for the borrowed text.

### 0. The shape of every exchange (the foundation)

yoshell defines exactly four tools and forces the model to call one of them every turn (`yo_build_tools_anthropic`, yo.c:2597; Chat Completions variant 2717; Responses variant 2842; forcing at 3344 / 3769):

| Tool | Input fields | Role | How the binary routes it |
| --- | --- | --- | --- |
| `command` | `command` (str), `explanation` (str), `pending` (bool) | emit a runnable command | prefill onto the line editor; print `explanation` first |
| `chat` | `response` (str) | answer / explain | print inline, return to a fresh prompt |
| `scrollback` | `lines` (int, ≤1000) | **request** recent terminal output | binary fetches it and re-calls the API (invisible to the user) |
| `docs` | (none) | **request** the tool's own documentation | binary supplies embedded help text and re-calls (invisible to the user) |

`command` and `chat` are *terminal* responses; `scrollback` and `docs` are mid-turn *requests* — the binary satisfies them and calls the API again with the result appended, capped at **3** such round-trips per turn (`yo_call_llm` passes `max_turns=3` to `yo_handle_requests`, yo.c:1382 / 1193). Output cap is 1024 tokens, raised to 4096 when server-side web search is enabled (`YO_MAX_TOKENS`, yo.c:67; 3307).

**Decision for `yo`:** adopt the four-tool shape verbatim, including `scrollback` and `docs` as model-initiated request tools. `scrollback` maps onto our multiplexer capture (tmux/zellij); with no multiplexer, the tool returns "unavailable" and the model proceeds without it. `docs` maps onto help text embedded in the binary. Force tool use (`tool_choice` "any"/"required") so prose can never land on the command line.

### 1. Command vs. Q&A disambiguation — RESOLVED

**The mode is the tool name.** `command` → prefill; `chat` → print. Parsing is a string→enum map (`yo_response_type_from_string`, yo.c:4495); there is no `mode` field that can contradict the payload.

Reliability is bought in two layers:

- **Tool descriptions as guardrails.** The `command` tool is described: *"CRITICAL: If you recommend any command, you MUST use this tool. Do NOT respond with chat that suggests a command"* (yo.c:2731). The `chat` tool: *"use ONLY when no command is needed… Do NOT include command suggestions here."*
- **System-prompt bias, applied asymmetrically per provider** — useful empirical finding. Anthropic gets a *minimal* base prompt (bashline.c:1030); its tool descriptions alone are enough. OpenAI and Kimi get a long "tuned prompt" (yo.c:3562 / 3639) because, in the code's own comment, *"Responses API models tend to over-use chat for things a shell assistant should answer with commands."* That tuned prompt states the rule and lists worked examples:
  > When in doubt between command and chat, ALWAYS choose command. Use chat for: greetings and casual conversation; abstract conceptual questions. If the user's question can be answered by running a command (cat, grep, sysctl, find, ls, echo…), you MUST use command.
  > • "what is the coredump pattern" → `cat /proc/sys/kernel/core_pattern`
  > • "how much disk space is left" → `df -h`
  > • "what ports are open" → `ss -tlnp`

**Decision for `yo`:** route by tool name. Do not expect one universal system prompt to behave identically across providers — Claude needs almost no command-biasing, GPT-class models need a lot. Plan for a per-provider prompt layer from day one, and **port yoshell's two prompt variants directly** — its minimal base prompt for Anthropic, its worked-example-heavy tuned prompt for OpenAI-style providers.

### 2. Parseable command output — RESOLVED (largely dissolved)

Tool calling makes the command a typed field, so the fence/prose/hedge failures the notes feared cannot happen. OpenAI additionally runs `strict: true` + `additionalProperties: false` (yo.c:2885) for server-side schema enforcement. What remains are three model misbehaviours yoshell still had to patch — cheap and worth copying:

1. **Missing `explanation`.** `command` requires `command` + `explanation`; if the model omits the explanation, yoshell re-prompts **once** with a tool result: *"Your command response is missing the required explanation field… respond again with the same command but include a brief explanation"* (`yo_retry_for_explanation`, yo.c:4432; driver 1304). One shot, then it proceeds with what it has.
2. **Multiple tool calls in one turn** (observed on Anthropic). Detected and re-prompted: *"You provided multiple tool calls. Please respond with exactly one"* (yo.c:4372).
3. **Over-long commands / here-docs.** Prevented by instruction, not parsing: *"Keep commands short and readable (prefer single-line). Do NOT emit large here-docs or long multi-line scripts. If a solution would be long, split into multiple steps using pending=true"* (yo.c:2731).

**Decision for `yo`:** lean entirely on tool calling for extraction; keep the one-shot explanation retry and the single-tool-call guard as robustness; carry the "no here-docs — split instead" instruction (it also feeds Q4).

### 3. Response schema — RESOLVED

The concrete field set, versus our original guesses:

| Field | Where | Notes |
| --- | --- | --- |
| `command` | `command` tool | the runnable string |
| `explanation` | `command` tool | shown to the user *before* the command; required |
| `pending` | `command` tool | the continuation signal (Q4); boolean |
| `response` | `chat` tool | prose answer |
| `lines` | `scrollback` tool | how much terminal output the model wants |
| ~~`mode`~~ | — | **not a field** — it's the tool identity |

**Session memory** stores, per exchange (`yo_history_add`): the query, response type, response content, the tool-use id, whether it was **executed**, whether it was **pending**, and any provider reasoning content. On each call the history is replayed into the request as reconstructed `tool_use`/`tool_result` message pairs, where the tool result literally encodes *"User executed the command"* vs. *"User did not execute the command"* (`yo_add_history_to_messages`, yo.c:5378). Defaults: `history_limit` 10 exchanges, `token_budget` 4096.

**Safety model.** Deliberately, there is no danger/confirmation field and no destructive-command detection anywhere in yoshell. Safety *is* the prefill-and-wait contract: nothing runs until the user reads it and presses Enter. yoshell draws the line there elegantly; we follow it. The tool is for adults.

**Decision for `yo`:** adopt the schema as-is. Persist the same per-exchange record (including executed/pending) and replay it as tool_use/tool_result pairs with the executed-status encoded in the tool result — that encoding is precisely what lets the model reason about what actually happened across turns.

### 4. Multi-step / continuation — RESOLVED, and it ports ~1:1

**The signal.** A `command` response with `pending: true` (yo.c:2621). The prompt forces granularity: *"When a task has sequential steps… you MUST use pending=true and issue ONE command at a time. NEVER combine steps into a single compound command (no && chains or semicolons). Each step its own command with pending=true (except the last, pending=false)"* (yo.c:3594).

**yoshell's in-process state machine** (all inside readline — the reason the fork existed):

1. `rl_yo_accept_line` (yo.c:1537) handles a `yo …` line: calls the LLM, gets `command` + `pending:true`, prefills via `rl_replace_line`, sets `yo_last_was_command` and `yo_continuation_active`, arms a SIGINT cleanup.
2. The user presses Enter (after any edits). On the *next* accept-line, yoshell sees the prior line was a generated command, marks the history entry executed, and **saves the exact buffer the user ran** (`yo_last_executed_command = rl_line_buffer`, yo.c:1559). It installs a one-shot `rl_startup_hook = yo_continuation_hook`.
3. The command runs; output lands on the terminal. At the next prompt the startup hook fires (`yo_continuation_hook`, yo.c:1415): it grabs **200 lines** of scrollback and builds a synthetic `[continuation]` user message carrying that output.
4. The LLM returns the next `command` (possibly `pending:true` again) or a `chat`. Prefill, wait, repeat until a non-pending response.

**How state carries — all three mechanisms at once:** session-memory replay (prior tool_use/tool_result incl. executed status) + a *fresh* 200-line scrollback grab + the explicit `[continuation]` message threading the output in. Step N+1 therefore sees step N's command *and* its result.

**Editing reconciliation.** Because step 2 saved the actually-executed buffer, the continuation message diffs intent against reality (yo.c:1448):
> `[continuation] You suggested: <X>` / `The user edited and executed: <Y>` / `Here is the terminal output: …`

so the model re-plans against what really ran, not against a command it proposed but the user changed.

**Termination.** Any of: a `command` with `pending:false`; a `chat`; an empty line; a new `yo …` query; Ctrl-C; or the 3-round request cap (yo.c:1505 / 1554 / 1164).

**UX.** Prefill-and-wait at every step — nothing auto-chains. This is exactly the "scalpel" model we wanted, and it falls out for free: the loop only re-engages *after* the user has run a step, because that's what fires the startup hook.

#### Porting it to standalone `yo`

The earlier worry that continuation needs a "tighter integration loop we don't have" was overstated. The bash/zsh/PowerShell line editors expose the very primitives yoshell uses, so the loop reproduces almost 1:1 from the integration snippet — **no fork required.** Each shell gives us (a) a prefill API, (b) a pre-prompt/startup hook to fire the continuation call when the prompt redraws, and (c) a way to capture the just-executed command:

| Shell | Prefill | Pre-prompt / startup hook | Capture executed command |
| --- | --- | --- | --- |
| bash | `READLINE_LINE` / `READLINE_POINT` (in a `bind -x` widget) | `PROMPT_COMMAND` | `DEBUG` trap, or `fc -ln -1` |
| zsh | `BUFFER=…; zle redisplay` | `precmd` | `preexec` (`$1` = the exact line) |
| PowerShell | `[Microsoft.PowerShell.PSConsoleReadLine]::Insert()` | `prompt` function | Enter-chord handler + `GetBufferState`, or `Get-History -Count 1` |

**The one nuance** the notes flagged — getting the exact *post-edit* command the way `rl_line_buffer` hands it to yoshell at accept time — is real but minor, and resolves the way you suggested: **fold it into terminal scraping.** The command the user actually ran is echoed into the very scrollback we already capture for that step's output, so a single capture yields both "what ran" and "what it produced." And it only matters when there *is* output to continue against — i.e. when a multiplexer is present — which is exactly when scraping is available. The two are naturally paired; it isn't a special case.

For the no-multiplexer path (where there's no output to continue against anyway, so continuation is already degraded), the per-shell capture hooks in the table — `preexec`, the `DEBUG` trap, PSReadLine history — still hand us the exact command directly, so the suggested-vs-executed diff survives even without scrollback if we want it. And it's a nicety regardless: if the scrollback already echoes the command, the model infers the edit on its own; worst case it simply works from the output. Low stakes.

**Decision for `yo`:** implement continuation as the per-shell triple above, mirroring yoshell's state machine (prefill → on execute, mark executed + arm the startup hook → capture scrollback → `[continuation]` call → repeat until `pending:false`). Recover the executed command primarily from the scrollback capture, with the shell's `preexec`/history hook as the no-multiplexer fallback. Build and prove the loop on **PowerShell** first (our first target — see [implementation plan](#implementation-plan)), then port to bash and zsh.

---

## Multi-step continuation (as built)

The continuation we shipped is the **exit-code** variant planned in the Q4 study above — output-feedback (scrollback) is still deferred. End to end:

**The signal.** Every `command` result carries a `pending` boolean. `pending:false` ends the turn; `pending:true` means "run this, then I'll give the next step."

**The state.** On a `pending` command the binary returns a base64 **state blob** — the original request plus the commands run so far — which the snippet stashes in `$env:YO_STATE`. The binary stays **pure**: prior state in via that env var, new state out via the result. No files, no session id, no daemon. (`internal/llm/state.go`)

**The loop:**
1. `yo <text>` → model returns a command with `pending:true` + state. The snippet prints the explanation, prefills the command, sets `$env:YO_STATE`, and **arms** (`$global:YoArmed`).
2. The wrapped `prompt` function (main runspace, so it can read `Get-History` and `$?`) captures a history baseline at the next prompt — it does *not* fire yet.
3. You run the prefilled command; history advances.
4. The next `prompt` sees the advance, reads `$?` as the exit code, and calls `yo.exe --continue --exit <0|1>` (inheriting `$env:YO_STATE`).
5. The binary decodes the state, records "step N → exit C", synthesizes a plain-text continuation turn (original request + steps so far + "give the next step or finish"), and asks the model.
6. Model returns the next command (`pending:true` to keep going, or `pending:false`/`chat` to end). Repeat from 1.

**Two hooks, split by capability:** the wrapped `prompt` *detects the run and fetches the next step* (it can see history + `$?`); the one-shot OnIdle *prefills* each step (it can call `Insert()`). The prefill handler unregisters itself after firing once, so it can't double-insert.

**Feedback between steps:** the exit code (`$?` → 0/1) always, plus — inside zellij — the step's actual terminal **output**, folded in by the same opportunistic scrollback capture the initial query uses (by the time `--continue` fires at the next prompt, the screen already shows the command and its result). So in a multiplexer the model reacts to real output; outside one, the exit code is the floor. Independently, the model also sees the command the user *actually ran*: the snippet captures it from `Get-History` and passes it via `$env:YO_RAN`, so an edited prefill is reconciled — the turn renders `ran Y   (you suggested: X)`. That recovers yoshell's edit-reconciliation, and it's the one feedback signal that survives even with no scrollback (no multiplexer). This was the long-deferred "output-fed continuation" — and it turned out to be one line, since the capture already existed and only needed to ride the `--continue` path. Two caveats: the whole screen is folded in (not a step-scoped slice), so the model correlates "command N → exit X" with the output at the bottom; and it is **redacted** before send by the same `internal/redact` pass the initial query uses (see [Secret redaction](#secret-redaction)).

**Termination / cancel:** a `pending:false` command, a `chat` reply, a new `yo …` (the function disarms first thing), or the step cap (`maxSteps` in `state.go`). Declining a prefilled step also cancels: **Ctrl+C** (a PSReadLine handler disarms when armed — cancel-only, so copy still works) or a **bare Enter** (the prompt sees no new command since arming and disarms). All of yoshell's termination gestures are now covered.

**Whether it triggers is the model's call, and stochastic.** The same request may chain into one command (`pending:false`) on one run and split into steps on another. PowerShell's expressiveness (inline `if` / `ForEach-Object`) lets the model solve many "conditional" tasks in a single command, so `pending` engages mainly for genuinely sequential work. Pushing harder toward `pending` is a prompt knob if we decide we want it firing more often.

---

## Implementation plan

### First runnable version (v0.1) — Windows + PowerShell, Anthropic

**Goal:** typing `yo <text>` at a PowerShell prompt returns either a command **prefilled on the next line** (editable; runs on Enter) or a printed answer. Single-shot — no scrollback, continuation, or memory yet. The smallest thing that proves the concept end to end.

**Deferred, to keep v0.1 small:** scrollback, multi-step continuation, session memory, the `scrollback`/`docs` tools, extra providers, other shells/OSes, signing/distribution, secret redaction.

Milestones (M0 and M1 are independent; M0 is the long pole — start it first):

- **M0 — PowerShell prefill spike (the #1 unknown).** Before any API work, prove we can place text on the *next* prompt's edit buffer from a function that has already returned. yoshell gets this free inside readline (`rl_startup_hook`); PowerShell offers no clean "set next line" hook, and `[Microsoft.PowerShell.PSConsoleReadLine]::Insert()` only works while the editor loop is live. Candidates, in order:
  1. a `Register-EngineEvent PowerShell.OnIdle` handler that injects pending text on the next idle — *verify it reaches the live edit buffer*; keeps the `yo <text>`⏎ UX;
  2. a `Set-PSReadLineKeyHandler` chord (how Atuin/PSFzf inject text — proven), trading the pure-command UX for a keypress;
  3. fallback: print + copy to clipboard (degraded).
  *Deliverable:* a stub puts `Get-ChildItem -Recurse` on the next prompt, editable. If neither (1) nor (2) works acceptably, that reshapes the UX — better to learn it now than after building the brain.

- **M1 — Go binary (testable with no shell).**
  - CLI `yo <text...>` (join args; also read stdin).
  - Config read on each call: `~/.yoconf` (provider/model/key/base_url) + the `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` env var for the key; home via `os.UserHomeDir()` (→ `%USERPROFILE%`).
  - Anthropic Messages client (`net/http` + `encoding/json`): port yoshell's **minimal Anthropic base prompt verbatim**; define the `command` + `chat` tools; `tool_choice:{"type":"any"}`; POST; parse the returned `tool_use`.
  - Binary↔snippet contract: one JSON line on **stdout** — `{"type":"command",…}` / `{"type":"chat",…}` / `{"type":"error",…}`; **stderr** carries only the transient "thinking…" indicator, so captured stdout stays clean.
  - *Deliverable:* `yo.exe "list files over 100MB"` prints the JSON — fully testable without PowerShell.

- **M2 — Wire the snippet to the binary.** `function yo { $r = & yo.exe @args | ConvertFrom-Json; … }` — `command` → print `explanation`, prefill `$r.command` via M0; `chat` → `Write-Host $r.response`; `error` → red `Write-Host`. Install: add `yo --init powershell | Out-String | iex` to `$PROFILE` — the snippet is embedded in the binary (`shell/embed.go`) and emitted by `--init`, so it stays version-locked; dot-sourcing the file still works for dev.

- **M3 — Polish to "runnable."** Thinking indicator during the call; friendly errors (missing key / network / API) with no prefill; Ctrl-C cancels the in-flight request; sane defaults (model, `max_tokens`).

**Done when:** on a clean Windows box — drop in the binary, add the profile snippet, set `$env:ANTHROPIC_API_KEY` — `yo find every pdf modified this week` prefills a working `Get-ChildItem` one-liner you can run with Enter.

### After v0.1 (rough order)

1. ~~**Scrollback (Windows-native).**~~ **Done** — zellij only (`zellij action dump-screen --full --path <file>`), folded into the query as a context block (`internal/scrollback` + `llm.WithTerminalContext`), ANSI-stripped, last 200 lines. No `Start-Transcript`/console-buffer fallback yet. Now folded into continuation steps too (the `--continue` path), not just the initial query, and the captured text is redacted before send.
2. ~~**Session memory.**~~ **Done** — per-session JSON store (`internal/session`), capped + swept, folded into the initial query via `llm.WithSessionMemory`; default on (`memory false` or an empty `$env:YO_SESSION` disables). See [Session memory (as built)](#session-memory-as-built). Standalone commands store the offered command only (executed-capture for standalone deferred).
3. ~~**Multi-step continuation.**~~ **Done** — exit-code feedback, env-var state; see [Multi-step continuation (as built)](#multi-step-continuation-as-built). **Output-fed continuation** — folding each step's scrollback into the `--continue` call — is now done too, as a one-liner on top of item 1.
4. ~~**Second provider** (OpenAI Responses, with the worked-example tuned prompt).~~ **Done.** Next: **`docs` tool** (embed help so "how do I configure yo?" is answered from real docs, not guessed).
5. **Breadth:** bash + zsh snippets; macOS + Linux builds (where tmux/zellij scrollback "just works"). Prose word-wrapping already lives in the binary (`internal/textwrap`, fed by `--width`), so a port just reports its width; only color stays per-snippet.
6. **Distribution & hardening:** winget/scoop, code signing.

### Housekeeping (carry-over)

- README (when one exists): one line stating `yo` is a GPLv3 derivative of `pizlonator/yosh`.
- Add `SPDX-License-Identifier: GPL-3.0-or-later` headers once code lands.

---

## Productionization roadmap (Windows feature-complete -> shippable)

The feature work is done (NL->command, multi-step + offered/executed, scrollback, redaction, session memory). What remains is turning "works for me" into "installable by a stranger." Ordered; **signing has procurement lead time, so kick that off in parallel early.**

1. **CLI surface + installer.** **Done:** `--help` (curated), `--version` (ldflags `-X main.version=<tag>` in CI, `debug.ReadBuildInfo` fallback), `--config`, and `--init powershell` — which emits the `shell/yo.ps1` snippet embedded via `//go:embed` (`shell/embed.go`), version-locked to the binary. Install is now the one-liner `yo --init powershell | Out-String | iex` in `$PROFILE`. Human-facing flags print plain text, never the stdout JSON contract. **Also done:** `yo --setup` / `--uninstall` (flags, not bare subcommands — those would collide with NL queries) — an idempotent checklist run via pwsh (the embedded `shell/setup.ps1`, shelled to with `--init`-style env handoff): adds the init line to `$PROFILE`, checks pwsh 7+/PSReadLine and upgrades it `-Scope CurrentUser` (no admin), adds the binary's dir to user PATH if missing, prompts for provider+key (sets a User env var), ends in `yo --check`. The PowerShell-native steps live in `setup.ps1` because finding `$PROFILE` / querying PSReadLine can't be done from Go; setup runs under the *invoking* shell (parent-process detection, `cmd/yo/parent_windows.go`) so it wires the right profile on **5.1 or 7+**. **Confirm-before-change: done.** Each modifying step (add to user PATH / upgrade PSReadLine / add the `$PROFILE` line / set the key env var) now previews exactly what it will do and asks first via a `Confirm` helper that defaults to yes (Enter accepts); a "no" skips *only that step* and setup proceeds — declining one never aborts the run. The provider prompt is a `1/2` menu (1 = Anthropic, 2 = OpenAI; typed names still accepted), not free-typed text. `--uninstall` likewise confirms before editing `$PROFILE`. Read-only steps (PATH already resolves, PSReadLine current, already wired, key already set, final `--check`) ask nothing. **Exit codes documented** in `--help`: `0` success, `1` runtime error (config / key / network / API), `2` usage error (no query, or an unknown `--init` shell) — `2` is the Unix usage-error convention, broader than the original "no query" note. **Item 1 is now complete.**
2. **README + license/docs.** README with the GPLv3-derivative statement made *user-visible*, install/usage/config, the safety stance, and an explicit no-telemetry note. The deferred **`docs` config blurb** (answer "how do I configure yo" from embedded help -- a prompt blurb, no tool-request loop). Full transitive-license aggregation for `NOTICE` (`go-licenses`) + an **SBOM** (`cyclonedx-gomod`). Verify SPDX header coverage; settle the prompt.go gofmt convention (it was committed gofmt-clean).
3. **Polish.** Friendly-error audit (missing key / network / API 4xx-5xx / bad config, no prefill); confirm Ctrl-C cancels an in-flight request; verify the default model ids are current; `yoconf.example` documents every directive incl `memory`; `-trimpath -ldflags "-s -w"` to shrink the ~15 MB binary.
4. **CI.** GH Actions matrix, windows amd64 + arm64 now (CGO-free -> cross-compile is trivial), darwin/linux later. PR checks: `go build/vet/test` plus a `. yo.ps1` parse smoke on a Windows runner -- the *only* automated guard for the snippet (interactive bits can't be CI-tested). Releases: binaries + sha256 + SBOM + `actions/attest-build-provenance` (release attestation; `gh attestation verify`).
5. **Signing.** Authenticode -- SmartScreen flags unsigned exes. OSS-realistic: SignPath Foundation (free for OSS) or Azure Trusted Signing (~$10/mo); EV cert otherwise. Sign in CI. macOS notarization (Apple Developer ID) is a later, separate track.
6. **Packaging.** scoop (easiest; portable exe + manifest, autoupdate from Releases; dev audience) + winget (broader). `go install github.com/martona/yo/cmd/yo@latest` works today if the repo is public -- mention it in the README now. **Skip MSIX**: its sandbox fights a CLI that edits `$PROFILE`, needs PATH, and hooks the shell; revisit only for a Store presence. chocolatey optional.
7. ~~**Zellij nudge.**~~ **Dropped on Windows** -- the native console capture (built, see the scrollback note below) covers the no-multiplexer case, so there is nothing to nudge toward. A POSIX nudge toward tmux/zellij stays a *possible* future idea (no native fallback there), but it is optional, not planned.

### Scrollback sources, and the no-multiplexer question

Resolved screen text comes only from whoever ran the terminal state machine. That differs by platform:

- **Windows: built** as the non-zellij fallback (`internal/scrollback/console_windows.go`) -- the OS console keeps a readable screen buffer, queried on demand via `ReadConsoleOutputCharacterW` (non-ambient, no daemon). **Catch, now measured:** under **Windows Terminal / ConPTY** the inferior sees only the viewport (a 42-row buffer for a 42-row window -- WT keeps its own scrollback and does not expose it); classic **conhost** exposes the full buffer (9001 rows). The viewport still covers the primary "why did THAT fail" case (the error is on the visible screen), and it is justified precisely because a multiplexer is culturally uncommon/awkward on Windows.
- **macOS / Linux:** **no equivalent.** The screen lives in the terminal emulator's private memory; the kernel PTY is a byte pipe with no buffer, and there is no portable read API (exceptions are emulator-specific -- iTerm2/kitty remote control -- or the Linux raw text console's `/dev/vcsa`). So the portable source is a **multiplexer** (tmux/zellij), which is culturally normal there -- a fair ask.

So scrollback is per-platform, both feeding `internal/scrollback`: **Windows -> console API (primary) + multiplexer (optional); Unix -> multiplexer only.**

**Rejected: our own PTY proxy with an in-memory buffer.** That is precisely what tmux/zellij *are*; building one means reimplementing a terminal emulator (VT state machine, PSReadLine fidelity, resize/mouse/paste, hot-path latency, a long-lived daemon, an IPC channel to reach `yo` invocations, per-OS native code) **and** sitting always in the keypath -- a direct violation of the core "never ambient" principle. Lean on multiplexers for deep history instead.

**Rejected: `Start-Transcript`.** Global session state + a continuous file, and it records the linear byte stream (every repaint/spinner), not the resolved screen -- reintroducing exactly the pollution the multiplexer/console-buffer approach avoids.