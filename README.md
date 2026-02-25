# claude-tokens

A small, dependency-free Go utility that displays your Claude API token usage and rate limit status. It reads your existing Claude Code OAuth credentials, automatically refreshes expired tokens, and outputs a compact one-line summary suitable for status bars like [Waybar](https://github.com/Alexays/waybar/wiki).

## Prerequisites

You must be logged in to Claude Code. If you haven't already, run:

```
claude login
```

This creates the credentials file at `~/.claude/.credentials.json` that `claude-tokens` reads.

## Installation

```
go install github.com/erikh/claude-tokens@latest
```

Or build from source:

```
git clone https://github.com/erikh/claude-tokens.git
cd claude-tokens
go build -o claude-tokens .
```

## Usage

```
claude-tokens [-s threshold] [-w threshold] [-W] [-r] [-j] [-p shell] [FORMAT ...]
```

Any arguments after the flags are joined with spaces and used as a custom format string (see [Custom Format](#custom-format) below). If no arguments are given, the default display is used.

### Flags

| Flag | Default | Description                                                                 |
| ---- | ------- | --------------------------------------------------------------------------- |
| `-s` | `90`    | Show session reset time when 5-hour usage exceeds this percentage           |
| `-w` | `80`    | Show weekly usage info when 7-day usage exceeds this percentage             |
| `-W` | off     | Always show weekly usage (appended as `: Weekly: <pct>%`)                   |
| `-r` | off     | Show remaining capacity instead of usage (e.g. `55% left` instead of `45%`) |
| `-j` | off     | Output usage data as JSON                                                   |
| `-p` | off     | Wrap ANSI codes for prompt use: `zsh` (`%{...%}`) or `bash` (`\001...\002`) |

### Output

The program prints a single line with your current token usage:

```
Claude Pro 45%
```

With `-r`, it shows remaining capacity instead:

```
Claude Pro 55% left
```

When session usage exceeds the `-s` threshold, the reset time is appended:

```
Claude Pro 92% (resets 2:15pm)
```

When weekly usage exceeds the `-w` threshold, weekly info is shown in brackets:

```
Claude Pro 92% (resets 2:15pm) [week: 85% (resets Thu 10:30am)]
```

With `-W`, weekly usage is always displayed at the end, regardless of threshold:

```
Claude Pro 45%: Weekly: 13%
```

Combined with `-r`:

```
Claude Pro 55% left: Weekly: 87% left
```

With `-j`, output is JSON containing both session and weekly utilization percentages with their reset times:

```json
{
    "session": {
        "utilization": 30,
        "resets_at": "2026-02-25T02:00:00.596989+00:00"
    },
    "weekly": {
        "utilization": 13,
        "resets_at": "2026-03-03T07:00:00.597012+00:00"
    }
}
```

Fields are omitted from JSON when the corresponding usage bucket is unavailable. Reset times are omitted when not present.

The fields are:

- **plan** -- your subscription type, capitalized (e.g. `Pro`, `Max`, `Free`)
- **session %** -- token usage in the current 5-hour window (or remaining capacity with `-r`)
- **session reset** -- when the 5-hour window resets (only shown above threshold)
- **week %** -- token usage in the 7-day window (shown above threshold with `-w`, or always with `-W`; remaining with `-r`)
- **week reset** -- when the 7-day window resets (only shown above threshold in bracket format)

All reset times are displayed in your local timezone (human-readable output) or as raw RFC3339 timestamps (JSON output).

### Custom Format

Any trailing arguments are joined with spaces and interpreted as a format string using printf-style verbs. When a format string is provided, it takes precedence over the default display and flags like `-s`, `-w`, `-W` (but `-j` still takes precedence over everything). The `-r` flag is respected by format verbs.

| Verb      | Expands to                                        | Example                             |
| --------- | ------------------------------------------------- | ----------------------------------- |
| `%p`      | Plan name (capitalized)                           | `Pro`                               |
| `%s`      | Session utilization                               | `45%` or `55% left` (respects `-r`) |
| `%S`      | Session reset time (always)                       | `2:15pm`                            |
| `%t`      | Session reset (only when usage >= `-s` threshold) | `resets 2:15pm` or empty            |
| `%w`      | Weekly utilization                                | `13%` or `87% left` (respects `-r`) |
| `%W`      | Weekly reset time (always)                        | `Thu 10:30am`                       |
| `%T`      | Weekly reset (only when usage >= `-w` threshold)  | `resets Thu 10:30am` or empty       |
| `%%`      | Literal `%`                                       | `%`                                 |
| `%{spec}` | ANSI color escape (see [Colors](#colors) below)   | `\x1b[31m`                          |

**Borders:** Any characters placed between `%` and a verb act as a border that wraps the output. The left (opening) side uses the characters as-is; the right (closing) side mirrors paired characters and reverses the string. Borders work with all format verbs.

Paired characters that get mirrored:

| ASCII                           | Unicode                                                          |
| ------------------------------- | ---------------------------------------------------------------- |
| `(` ↔ `)`, `[` ↔ `]`, `<` ↔ `>` | `«` ↔ `»`, `‹` ↔ `›`, `⟨` ↔ `⟩`                                  |
|                                 | `←` ↔ `→`, `⇐` ↔ `⇒`, `⟵` ↔ `⟶`, `⟸` ↔ `⟹`                       |
|                                 | `◀` ↔ `▶`, `◁` ↔ `▷`, `◂` ↔ `▸`, `◃` ↔ `▹`, `▲` ↔ `▼`, `△` ↔ `▽` |
|                                 | `❨` ↔ `❩`, `❪` ↔ `❫`, `❬` ↔ `❭`, `❮` ↔ `❯`, `❰` ↔ `❱`            |
|                                 | `⌈` ↔ `⌉`, `⌊` ↔ `⌋`                                             |

Non-paired characters repeat unchanged. `{` is reserved for color specs.

| Format   | Output        | Description                                      |
| -------- | ------------- | ------------------------------------------------ |
| `%s`     | `45%`         | No border                                        |
| `%(s`    | `(45%)`       | Parentheses                                      |
| `%[s`    | `[45%]`       | Brackets                                         |
| `%-=s`   | `-=45%=-`     | Multi-char, reversed                             |
| `%-=[s`  | `-=[45%]=-`   | `[` mirrors to `]`, then reversed                |
| `%<<(s`  | `<<(45%)>>`   | `(` mirrors to `)`, `<` mirrors to `>`, reversed |
| `%«s`    | `«45%»`       | Unicode guillemets                               |
| `%⟨s`    | `⟨45%⟩`       | Mathematical angle brackets                      |
| `%◀s`    | `◀45%▶`       | Unicode triangles                                |
| `%←s`    | `←45%→`       | Unicode arrows                                   |
| `%«-=[s` | `«-=[45%]=-»` | Mixed Unicode and ASCII                          |

For `%t` and `%T`, the border only appears when usage meets the threshold — below threshold, the entire verb (including borders) disappears cleanly. Unknown verbs pass through as-is.

```
claude-tokens '%p: %s used, resets %S'
# Pro: 45% used, resets 2:15pm

claude-tokens 'Session: %s | Weekly: %w'
# Session: 45% | Weekly: 13%

claude-tokens -r '%p %s session, %w weekly'
# Pro 55% left session, 87% left weekly

claude-tokens %p %s %w
# Pro 45% 13%

claude-tokens -s 50 '%s %(t'
# 60% (resets 2:15pm)    — when session >= 50%
# 30%                    — when session < 50%, %(t disappears entirely

claude-tokens -s 50 '%s %-=[t'
# 60% -=[resets 2:15pm]=-    — multi-char border with bracket mirroring

claude-tokens '%«p %⟨s'
# «Pro» ⟨45%⟩    — Unicode borders on any verb
```

Note that shell quoting is optional — unquoted arguments are joined with spaces automatically. Use quotes if your format contains characters the shell might interpret.

### Colors

The `%{spec}` verb generates ANSI terminal color escape sequences using the same grammar as [colt](https://github.com/erikh/colt):

```
%{foreground [on background] [with attr ...]}
```

**Available colors** (for both foreground and background):

`black`, `red`, `green`, `yellow`, `blue`, `magenta`, `cyan`, `white`, `hiblack`, `hired`, `higreen`, `hiyellow`, `hiblue`, `himagenta`, `hicyan`, `hiwhite`

**Available attributes** (after `with`):

`reset`, `bold`, `faint`, `italic`, `underline`, `blinkslow`, `blinkrapid`, `reversevideo`, `concealed`, `crossedout`

```
claude-tokens '%{red}%p %s%{white with reset}'
# Outputs plan and session usage in red, then resets

claude-tokens '%{green with bold}%s%{white with reset} %{yellow}%w%{white with reset}'
# Session in bold green, weekly in yellow

claude-tokens '%{white on red with bold}%s%{white with reset}'
# Session usage in bold white on red background
```

### Prompt Escaping

When embedding `claude-tokens` output in a shell prompt (e.g. zsh `RPROMPT` or bash `PS1`), ANSI escape sequences must be marked as non-printing so the shell can calculate the correct cursor position. Without this, the shell counts escape bytes as visible characters and miscalculates the prompt width, causing display corruption.

The `-p` flag wraps all ANSI color codes emitted by `%{spec}` in the appropriate markers:

| Mode        | Wrapping          | Use case                          |
| ----------- | ----------------- | --------------------------------- |
| `-p zsh`    | `%{` ... `%}`     | zsh prompts (`PROMPT`, `RPROMPT`) |
| `-p bash`   | `\001` ... `\002` | bash `PS1` / readline prompts     |
| _(default)_ | _(none)_          | Terminals, status bars, scripts   |

In `-p zsh` mode, `%` characters in content (e.g. the `%` in `45%`) are also escaped as `%%` so the output can be used directly in zsh prompt strings without additional post-processing. Color wrappers (`%{...%}`) are emitted verbatim for zsh to interpret.

```
# zsh — colors wrapped in %{...%}, content % escaped as %%
claude-tokens -p zsh '%{cyan}%[s%{white with reset}'
# output: %{\x1b[36m%}[45%%]%{\x1b[37;0m%}
# zsh renders: [45%] in cyan with correct cursor position

# bash — colors wrapped in \001...\002 for correct PS1 width
claude-tokens -p bash '%{cyan}%[s%{white with reset}'

# raw — no wrapping, for terminal output or status bars
claude-tokens '%{cyan}%[s%{white with reset}'
```

### Examples

Show reset times earlier (when session hits 70% used, weekly hits 50%):

```
claude-tokens -s 70 -w 50
```

Always show all info (set thresholds to 0):

```
claude-tokens -s 0 -w 0
```

Always show weekly usage alongside session usage:

```
claude-tokens -W
```

Show remaining capacity instead of usage:

```
claude-tokens -r
```

Show remaining capacity with weekly always visible:

```
claude-tokens -r -W
```

Output as JSON (useful for scripting or piping to `jq`):

```
claude-tokens -j
```

Pretty-print JSON output:

```
claude-tokens -j | jq .
```

## Waybar Integration

Add a custom module to your Waybar config (`~/.config/waybar/config`):

```json
{
    "modules-right": ["custom/claude-tokens"],

    "custom/claude-tokens": {
        "exec": "claude-tokens -s 0 -w 0",
        "interval": 300,
        "format": "{}",
        "return-type": "",
        "tooltip": false
    }
}
```

This polls every 5 minutes (300 seconds) and displays the output in your bar. Adjust `interval` to taste -- the API is lightweight but there's no reason to poll more than once a minute or so. The `-s 0 -w 0` flags ensure all usage info is always visible; omit them to only see reset times when approaching limits.

You can style it with your Waybar CSS (`~/.config/waybar/style.css`):

```css
#custom-claude-tokens {
    color: #c4a7e7;
    padding: 0 10px;
}
```

## Shell Integration

Here's a way to use this tool as a part of your shell prompt:

```bash
case $TERM in
    linux)
    ;;
    screen|tmux)
      export TERM=tmux-256color
    ;;
    *)
        precmd() {
            time=$(date -d "$(stat /tmp/tokens | grep Modify | awk -F: '{ print $2":"$3":"$4 }')" +%s)
            if [ $? -ne 0 ] || [ $(($time + 300)) -lt $(date +%s) ]
            then
              claude-tokens -p zsh '%{cyan}Cur: %{bold white}%[s%{reset}%{bright red}%!t%{reset} %{yellow}Week: %{bold white}%[w%{reset}%{bright red}%!!T%{reset}' >/tmp/tokens
            fi

            export TOKENS=$(cat /tmp/tokens)
            export RPROMPT="[%B$TOKENS%b][%B%T%b]"

            case $TERM in
                tmux-256color)
                    print -Pn "\ek$PROMPT\e\\"
                    ;;
                vt100)
                    ;;
                *)
                    print -Pn "\e]0;$PROMPT\e\\"
                    ;;
            esac
        }

        preexec() {
            case $TERM in
                tmux-256color)
                    print -Pn "\ek$1\e\\"
                    ;;
                vt100)
                    ;;
                *)
                    print -Pn "\e]0;$1\e\\"
                    ;;
            esac
        }
        ;;
esac
```

## How It Works

1. Reads OAuth credentials from `~/.claude/.credentials.json`
2. If the access token is expired, refreshes it automatically and updates the credentials file
3. Queries the Anthropic usage API (`https://api.anthropic.com/api/oauth/usage`)
4. Parses the 5-hour session and 7-day weekly usage buckets
5. Prints a one-line summary to stdout

## License

MIT
