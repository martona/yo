# `yo` — Design Notes

A standalone, cross-platform LLM-enabled command assistant. Type `yo <natural language>` at your shell prompt; the request goes to an LLM API, a structured response comes back, and the relevant command is prefilled onto your shell's line editor — ready to run, edit, or cancel. Derived from [yoshell](https://github.com/pizlonator/yosh): it reuses yoshell's prompt design and response protocol directly, but takes its own architectural road for delivery — a standalone native binary instead of a shell fork.

`yo` is an acknowledged **derivative work** of yoshell, licensed **GPLv3** to match. It inherits the command name, the system prompts, and the response/continuation protocol from yoshell's `yo.c`; what it does *not* carry is the Bash/Readline C the original is built on. See [License & provenance](#license--provenance).

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

Native moves the cost from *duplicated runtime logic* (unbounded, ongoing) to *one-time build/distribution plumbing* (finite, solved): cross-compilation, three binary sets, macOS Gatekeeper signing, Windows SmartScreen flagging of unsigned exes, and an install story (`brew`, `cargo install`, release downloads, `winget`/`scoop`). All well-trodden — Atuin solves the identical set. The finite problem is the better one to own.

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
| Fil-C memory-safe build | Rust/Go memory-safe by construction |

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

This is fully available to a standalone binary — `tools` + `tool_choice` are just fields in the request body, identical from Rust/Go as from a forked shell. We adopt it wholesale. Detailed schemas, the per-provider prompt asymmetry, and the residual failure modes yoshell still had to patch are in [Resolved: prompt handling](#resolved-prompt-handling-studied-from-yoshell).

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

### Scrollback context — opt-in, via multiplexer

If you want the model to see what's on your screen ("why did that command fail?"), you opt in by running inside a terminal multiplexer:

- **tmux:** `tmux capture-pane -pS -1000`
- **zellij:** `zellij action dump-screen --full` (cross-platform)

This is elegant because both commands return the **already-resolved screen state** — cells, not the raw byte stream. The multiplexer is a real terminal emulator: it ran the state machine and collapsed all the redraw frames (Docker progress, cargo's live block, npm spinners, Atuin's repaints) into the one frame you actually saw. **All the transcript-pollution problems evaporate** because we never see the animation that produced the screen, only the screen.

Design choices:
- Scrollback is **strictly optional, not a cornerstone.** With a multiplexer present → rich clean context on all three platforms. Without one → fall back to no-scrollback (just the `yo` prompt + session memory), which works everywhere.
- This is what keeps Windows from being second-class: the feature that's genuinely hard on native Windows (tmux is WSL-only there; zellij runs but is less battle-tested) is exactly the one we made optional.
- v1 implementation: shell out. Detect `$TMUX` / `$ZELLIJ`, run the capture command, read stdout. (Control protocols are more robust but more work — defer.)
- Capture depth (`-1000`) configurable, probably default smaller.

### Secret redaction — a clean, separable, bolted-on layer

Explicitly **not** a cornerstone and **not** a v1 requirement. yoshell does nothing here; we can do better when the time comes, as a separable layer on the *read* path (when the outbound payload is assembled), since any captured buffer or transcript contains raw secrets.

Approaches, in increasing ambition (all deferred):
- Entropy-based detection for long high-entropy strings.
- Local secret-scanner or local model as the redaction stage, so detection happens on-device and only scrubbed text crosses the wire.
- Env-var hygiene: never serialize all of `env` — whitelist (PATH, PWD, OS), never blacklist.
- Preview/dry-run mode: show the exact post-redaction payload before sending.

Defense-in-depth, not perfection — say so in the docs.

---

## v1 scope

- **Targets:** native binary on macOS / Linux / Windows.
- **Shells:** bash + zsh + PowerShell.
- **Providers:** Anthropic + OpenAI, mirroring yoshell's key surfaces (`~/.anthropickey` / `~/.openaikey` / `~/.yoconf`-style config) as a convention.
- **Core loop:** `yo <text>` → API → parse → prefill.
- **Session memory:** in-process / per-session conversation context with a token budget.
- **Scrollback:** opt-in via tmux/zellij detection; graceful no-op fallback.
- **Secret redaction:** out of scope for v1; designed as a later separable layer.

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

**Decision for `yo`:** implement continuation as the per-shell triple above, mirroring yoshell's state machine (prefill → on execute, mark executed + arm the startup hook → capture scrollback → `[continuation]` call → repeat until `pending:false`). Recover the executed command primarily from the scrollback capture, with the shell's `preexec`/history hook as the no-multiplexer fallback. Build and prove the **bash** loop end-to-end first to de-risk, then port to zsh and PowerShell.

---

## Things to decide next

- **Licensing (settled — see [License & provenance](#license--provenance)):** ~~add the GPLv3 `LICENSE` and a `NOTICE` crediting `pizlonator/yosh`~~ **done** (`LICENSE` is byte-identical to upstream's GPLv3; `NOTICE` records the derivative-work relationship + the §5 change summary). Still to do: add the derivative-work + GPLv3 line to the README once one exists, and a per-file SPDX header (`SPDX-License-Identifier: GPL-3.0-or-later`) when code lands.
- ~~Resolve the prompt-handling open questions.~~ **Done** (above). What's left is porting and wiring, not open design:
  - Port yoshell's two system prompts **verbatim** (minimal for Anthropic, worked-example-heavy for OpenAI-style), then iterate from a known-good baseline.
  - Carry its tool definitions (`command` / `chat` / `scrollback` / `docs`) with forced `tool_choice`, plus the explanation-retry and single-tool-call guards.
- Skeleton: core loop (assemble request → API → tool-call dispatch → prefill/print) + bash/zsh/PowerShell integration snippets (prefill + startup hook + executed-command capture) + multiplexer-detect-and-capture.
- Continuation: build the bash loop first, end-to-end, before adding shell breadth.