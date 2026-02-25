package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var ansiRe = regexp.MustCompile(
	`\x1b\[[0-9;]*[a-zA-Z]` + // CSI sequences: \e[...m, \e[...H, etc.
		`|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)` + // OSC sequences: \e]...BEL or \e]...\e\\
		`|\x1b[()#][0-9A-Za-z]` + // charset select
		`|\x1b[a-zA-Z]`, // two-char escapes like \eM, \eD
)

func stripTermCodes(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

const oauthClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

var tokenEndpoint = "https://console.anthropic.com/v1/oauth/token"

type oauthCredentials struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken"`
	ExpiresAt        int64    `json:"expiresAt"`
	Scopes           []string `json:"scopes"`
	SubscriptionType string   `json:"subscriptionType"`
	RateLimitTier    string   `json:"rateLimitTier"`
}

type credentials struct {
	ClaudeAiOauth oauthCredentials `json:"claudeAiOauth"`
}

type usageBucket struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    *string `json:"resets_at"`
}

type usageResponse struct {
	FiveHour *usageBucket `json:"five_hour"`
	SevenDay *usageBucket `json:"seven_day"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

type jsonBucket struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at,omitempty"`
}

type jsonUsageOutput struct {
	Session *jsonBucket `json:"session,omitempty"`
	Weekly  *jsonBucket `json:"weekly,omitempty"`
}

func refreshToken(creds *credentials, credsPath string) error {
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": creds.ClaudeAiOauth.RefreshToken,
		"client_id":     oauthClientID,
	})

	resp, err := http.Post(
		tokenEndpoint,
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("refresh failed (%d): %s", resp.StatusCode, b)
	}

	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return fmt.Errorf("parsing refresh response: %w", err)
	}

	creds.ClaudeAiOauth.AccessToken = tok.AccessToken
	creds.ClaudeAiOauth.RefreshToken = tok.RefreshToken
	creds.ClaudeAiOauth.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).UnixMilli()

	data, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("marshaling credentials: %w", err)
	}

	return os.WriteFile(credsPath, data, 0600)
}

func formatReset(raw *string, layout string) string {
	if raw == nil {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, *raw)
	if err != nil {
		return ""
	}
	return t.Local().Format(layout)
}

type displayOptions struct {
	sessionPct   float64
	weeklyPct    float64
	alwaysWeekly bool
	remaining    bool
}

func formatPct(utilization float64, remaining bool) string {
	suffix := ""
	v := utilization
	if remaining {
		v = 100 - utilization
		suffix = " left"
	}
	return fmt.Sprintf("%.0f%%%s", v, suffix)
}

func formatJSON(usage usageResponse) (string, error) {
	out := jsonUsageOutput{}
	if usage.FiveHour != nil {
		b := jsonBucket{Utilization: usage.FiveHour.Utilization}
		if usage.FiveHour.ResetsAt != nil {
			b.ResetsAt = *usage.FiveHour.ResetsAt
		}
		out.Session = &b
	}
	if usage.SevenDay != nil {
		b := jsonBucket{Utilization: usage.SevenDay.Utilization}
		if usage.SevenDay.ResetsAt != nil {
			b.ResetsAt = *usage.SevenDay.ResetsAt
		}
		out.Weekly = &b
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func formatDisplay(usage usageResponse, plan string, opts displayOptions) string {
	if plan == "" {
		plan = "Unknown"
	} else {
		plan = strings.ToUpper(plan[:1]) + plan[1:]
	}

	out := "Claude " + plan

	if usage.FiveHour != nil {
		out += " " + formatPct(usage.FiveHour.Utilization, opts.remaining)

		if usage.FiveHour.Utilization >= opts.sessionPct {
			if s := formatReset(usage.FiveHour.ResetsAt, "3:04pm"); s != "" {
				out += fmt.Sprintf(" (resets %s)", s)
			}
		}
	}

	if usage.SevenDay != nil {
		if opts.alwaysWeekly {
			out += ": Weekly: " + formatPct(usage.SevenDay.Utilization, opts.remaining)
		} else if usage.SevenDay.Utilization >= opts.weeklyPct {
			week := "week: " + formatPct(usage.SevenDay.Utilization, opts.remaining)

			if s := formatReset(usage.SevenDay.ResetsAt, "Mon 3:04pm"); s != "" {
				week += fmt.Sprintf(" (resets %s)", s)
			}
			out += " [" + week + "]"
		}
	}

	return out
}

func formatCustom(format string, usage usageResponse, plan string, remaining bool) string {
	if plan == "" {
		plan = "Unknown"
	} else {
		plan = strings.ToUpper(plan[:1]) + plan[1:]
	}

	var out strings.Builder
	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 >= len(format) {
			out.WriteByte(format[i])
			continue
		}
		i++
		switch format[i] {
		case 'p':
			out.WriteString(plan)
		case 's':
			if usage.FiveHour != nil {
				out.WriteString(formatPct(usage.FiveHour.Utilization, remaining))
			}
		case 'S':
			if usage.FiveHour != nil {
				out.WriteString(formatReset(usage.FiveHour.ResetsAt, "3:04pm"))
			}
		case 'w':
			if usage.SevenDay != nil {
				out.WriteString(formatPct(usage.SevenDay.Utilization, remaining))
			}
		case 'W':
			if usage.SevenDay != nil {
				out.WriteString(formatReset(usage.SevenDay.ResetsAt, "Mon 3:04pm"))
			}
		case '%':
			out.WriteByte('%')
		default:
			out.WriteByte('%')
			out.WriteByte(format[i])
		}
	}
	return out.String()
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] [FORMAT ...]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Trailing arguments are joined and used as a format string.\n")
		fmt.Fprintf(os.Stderr, "Format verbs: %%p plan, %%s session%%, %%S session reset,\n")
		fmt.Fprintf(os.Stderr, "              %%w weekly%%, %%W weekly reset, %%%% literal %%\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	sessionPct := flag.Float64("s", 90, "show reset time when session usage exceeds this %")
	weeklyPct := flag.Float64("w", 80, "show weekly usage when it exceeds this %")
	alwaysWeekly := flag.Bool("W", false, "always show weekly usage")
	remaining := flag.Bool("r", false, "show remaining capacity instead of usage")
	jsonOutput := flag.Bool("j", false, "output usage as JSON")
	flag.Parse()

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	credsPath := filepath.Join(home, ".claude", ".credentials.json")
	data, err := os.ReadFile(credsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading credentials: %v\n", err)
		os.Exit(1)
	}

	var creds credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing credentials: %v\n", err)
		os.Exit(1)
	}

	if creds.ClaudeAiOauth.AccessToken == "" {
		fmt.Fprintln(os.Stderr, "no Claude OAuth credentials found; run 'claude login'")
		os.Exit(1)
	}

	if time.Now().UnixMilli() >= creds.ClaudeAiOauth.ExpiresAt {
		if err := refreshToken(&creds, credsPath); err != nil {
			fmt.Fprintf(os.Stderr, "token refresh failed: %v\nRun 'claude login' to re-authenticate.\n", err)
			os.Exit(1)
		}
	}

	req, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	req.Header.Set("Authorization", "Bearer "+creds.ClaudeAiOauth.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "API error %d: %s\n", resp.StatusCode, respBody)
		os.Exit(1)
	}

	var usage usageResponse
	if err := json.Unmarshal(respBody, &usage); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing usage: %v\n", err)
		os.Exit(1)
	}

	if args := flag.Args(); len(args) > 0 {
		formatStr := strings.Join(args, " ")
		fmt.Print(stripTermCodes(formatCustom(formatStr, usage, creds.ClaudeAiOauth.SubscriptionType, *remaining)))
		return
	}

	if *jsonOutput {
		s, err := formatJSON(usage)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error marshaling JSON: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(stripTermCodes(s))
		return
	}

	opts := displayOptions{
		sessionPct:   *sessionPct,
		weeklyPct:    *weeklyPct,
		alwaysWeekly: *alwaysWeekly,
		remaining:    *remaining,
	}
	fmt.Print(stripTermCodes(formatDisplay(usage, creds.ClaudeAiOauth.SubscriptionType, opts)))
}
