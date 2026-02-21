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
go install gitea.com/erikh/claude-tokens@latest
```

Or build from source:

```
git clone https://gitea.com/erikh/claude-tokens.git
cd claude-tokens
go build -o claude-tokens .
```

## Usage

```
claude-tokens [-s threshold] [-w threshold] [-r]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-s` | `90` | Show session reset time when 5-hour usage exceeds this percentage |
| `-w` | `80` | Show weekly usage info when 7-day usage exceeds this percentage |
| `-r` | off | Show remaining capacity instead of usage (e.g. `55% left` instead of `45%`) |

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
Claude Pro 12% (resets 2:15pm)
```

When weekly usage also exceeds the `-w` threshold, weekly info is shown too:

```
Claude Pro 12% (resets 2:15pm) [week: 38% (resets Thu 10:30am)]
```

The fields are:
- **plan** -- your subscription type, capitalized (e.g. `Pro`, `Free`)
- **session %** -- token usage in the current 5-hour window (or remaining capacity with `-r`)
- **session reset** -- when the 5-hour window resets (only shown above threshold)
- **week %** -- token usage in the 7-day window (only shown above threshold; or remaining with `-r`)
- **week reset** -- when the 7-day window resets (only shown above threshold)

All reset times are displayed in your local timezone.

### Examples

Show reset times earlier (when session hits 70% used, weekly hits 50%):

```
claude-tokens -s 70 -w 50
```

Always show all info (set thresholds to 0):

```
claude-tokens -s 0 -w 0
```

Show remaining capacity instead of usage:

```
claude-tokens -r
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

## How It Works

1. Reads OAuth credentials from `~/.claude/.credentials.json`
2. If the access token is expired, refreshes it automatically and updates the credentials file
3. Queries the Anthropic usage API (`https://api.anthropic.com/api/oauth/usage`)
4. Parses the 5-hour session and 7-day weekly usage buckets
5. Prints a one-line summary to stdout

## License

MIT
