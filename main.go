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
	"strings"
	"time"
)

const oauthClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

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

func refreshToken(creds *credentials, credsPath string) error {
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": creds.ClaudeAiOauth.RefreshToken,
		"client_id":     oauthClientID,
	})

	resp, err := http.Post(
		"https://console.anthropic.com/v1/oauth/token",
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

func main() {
	sessionPct := flag.Float64("s", 90, "show reset time when session usage exceeds this %")
	weeklyPct := flag.Float64("w", 80, "show weekly usage when it exceeds this %")
	remaining := flag.Bool("r", false, "show remaining capacity instead of usage")
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

	plan := creds.ClaudeAiOauth.SubscriptionType
	if plan == "" {
		plan = "Unknown"
	} else {
		plan = strings.ToUpper(plan[:1]) + plan[1:]
	}

	out := "Claude " + plan

	pct := func(utilization float64) string {
		suffix := ""
		v := utilization
		if *remaining {
			v = 100 - utilization
			suffix = " left"
		}
		return fmt.Sprintf("%.0f%%%s", v, suffix)
	}

	if usage.FiveHour != nil {
		out += " " + pct(usage.FiveHour.Utilization)

		if usage.FiveHour.Utilization >= *sessionPct {
			if s := formatReset(usage.FiveHour.ResetsAt, "3:04pm"); s != "" {
				out += fmt.Sprintf(" (resets %s)", s)
			}
		}
	}

	if usage.SevenDay != nil && usage.SevenDay.Utilization >= *weeklyPct {
		week := "week: " + pct(usage.SevenDay.Utilization)

		if s := formatReset(usage.SevenDay.ResetsAt, "Mon 3:04pm"); s != "" {
			week += fmt.Sprintf(" (resets %s)", s)
		}
		out += " [" + week + "]"
	}

	fmt.Println(out)
}
