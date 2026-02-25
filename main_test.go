package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func ptr(s string) *string { return &s }

func TestFormatPct(t *testing.T) {
	tests := []struct {
		name        string
		utilization float64
		remaining   bool
		want        string
	}{
		{"zero usage", 0, false, "0%"},
		{"half usage", 50, false, "50%"},
		{"full usage", 100, false, "100%"},
		{"fractional rounds down", 49.4, false, "49%"},
		{"fractional rounds up", 49.6, false, "50%"},
		{"remaining zero", 0, true, "100% left"},
		{"remaining half", 50, true, "50% left"},
		{"remaining full", 100, true, "0% left"},
		{"remaining fractional", 33.3, true, "67% left"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatPct(tt.utilization, tt.remaining)
			if got != tt.want {
				t.Errorf("formatPct(%v, %v) = %q, want %q", tt.utilization, tt.remaining, got, tt.want)
			}
		})
	}
}

func TestFormatDisplay_Default(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{Utilization: 30},
		SevenDay: &usageBucket{Utilization: 13},
	}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80}

	got := formatDisplay(usage, "max", opts)
	want := "Claude Max 30%"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatDisplay_EmptyPlan(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{Utilization: 10},
	}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80}

	got := formatDisplay(usage, "", opts)
	want := "Claude Unknown 10%"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatDisplay_AlwaysWeekly(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{Utilization: 30},
		SevenDay: &usageBucket{Utilization: 13},
	}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80, alwaysWeekly: true}

	got := formatDisplay(usage, "max", opts)
	want := "Claude Max 30%: Weekly: 13%"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatDisplay_AlwaysWeeklyRemaining(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{Utilization: 30},
		SevenDay: &usageBucket{Utilization: 13},
	}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80, alwaysWeekly: true, remaining: true}

	got := formatDisplay(usage, "max", opts)
	want := "Claude Max 70% left: Weekly: 87% left"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatDisplay_WeeklyThresholdExceeded(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{Utilization: 50},
		SevenDay: &usageBucket{Utilization: 85},
	}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80}

	got := formatDisplay(usage, "max", opts)
	want := "Claude Max 50% [week: 85%]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatDisplay_WeeklyThresholdNotExceeded(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{Utilization: 50},
		SevenDay: &usageBucket{Utilization: 70},
	}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80}

	got := formatDisplay(usage, "max", opts)
	want := "Claude Max 50%"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatDisplay_WeeklyThresholdWithReset(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{Utilization: 50},
		SevenDay: &usageBucket{
			Utilization: 85,
			ResetsAt:    ptr("2026-03-03T07:00:00.000000+00:00"),
		},
	}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80}

	got := formatDisplay(usage, "max", opts)
	// Reset time formatting depends on local timezone, so just check the structure
	if got == "Claude Max 50% [week: 85%]" {
		t.Error("expected reset time in output but got none")
	}
	if len(got) < len("Claude Max 50% [week: 85% (resets ") {
		t.Errorf("output too short, missing reset time: %q", got)
	}
}

func TestFormatDisplay_SessionResetShown(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{
			Utilization: 95,
			ResetsAt:    ptr("2026-02-25T02:00:00.000000+00:00"),
		},
		SevenDay: &usageBucket{Utilization: 13},
	}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80}

	got := formatDisplay(usage, "max", opts)
	// Should contain reset time since 95 >= 90
	if got == "Claude Max 95%" {
		t.Error("expected reset time in output but got none")
	}
}

func TestFormatDisplay_SessionResetNotShown(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{
			Utilization: 85,
			ResetsAt:    ptr("2026-02-25T02:00:00.000000+00:00"),
		},
		SevenDay: &usageBucket{Utilization: 13},
	}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80}

	got := formatDisplay(usage, "max", opts)
	want := "Claude Max 85%"
	if got != want {
		t.Errorf("got %q, want %q (reset should be hidden below threshold)", got, want)
	}
}

func TestFormatDisplay_NilBuckets(t *testing.T) {
	usage := usageResponse{}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80}

	got := formatDisplay(usage, "pro", opts)
	want := "Claude Pro"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatDisplay_NilFiveHourWithWeekly(t *testing.T) {
	usage := usageResponse{
		SevenDay: &usageBucket{Utilization: 50},
	}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80, alwaysWeekly: true}

	got := formatDisplay(usage, "max", opts)
	want := "Claude Max: Weekly: 50%"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatDisplay_Remaining(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{Utilization: 30},
		SevenDay: &usageBucket{Utilization: 13},
	}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80, remaining: true}

	got := formatDisplay(usage, "max", opts)
	want := "Claude Max 70% left"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatJSON_BothBuckets(t *testing.T) {
	reset5h := "2026-02-25T02:00:00.000000+00:00"
	reset7d := "2026-03-03T07:00:00.000000+00:00"
	usage := usageResponse{
		FiveHour: &usageBucket{Utilization: 30, ResetsAt: &reset5h},
		SevenDay: &usageBucket{Utilization: 13, ResetsAt: &reset7d},
	}

	got, err := formatJSON(usage)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed jsonUsageOutput
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if parsed.Session == nil {
		t.Fatal("expected session to be present")
	}
	if parsed.Session.Utilization != 30 {
		t.Errorf("session utilization = %v, want 30", parsed.Session.Utilization)
	}
	if parsed.Session.ResetsAt != reset5h {
		t.Errorf("session resets_at = %q, want %q", parsed.Session.ResetsAt, reset5h)
	}

	if parsed.Weekly == nil {
		t.Fatal("expected weekly to be present")
	}
	if parsed.Weekly.Utilization != 13 {
		t.Errorf("weekly utilization = %v, want 13", parsed.Weekly.Utilization)
	}
	if parsed.Weekly.ResetsAt != reset7d {
		t.Errorf("weekly resets_at = %q, want %q", parsed.Weekly.ResetsAt, reset7d)
	}
}

func TestFormatJSON_NoResetTimes(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{Utilization: 0},
		SevenDay: &usageBucket{Utilization: 0},
	}

	got, err := formatJSON(usage)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed jsonUsageOutput
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if parsed.Session.ResetsAt != "" {
		t.Errorf("expected empty resets_at, got %q", parsed.Session.ResetsAt)
	}
	if parsed.Weekly.ResetsAt != "" {
		t.Errorf("expected empty resets_at, got %q", parsed.Weekly.ResetsAt)
	}
}

func TestFormatJSON_NilBuckets(t *testing.T) {
	usage := usageResponse{}

	got, err := formatJSON(usage)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "{}"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatJSON_OnlySession(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{Utilization: 42},
	}

	got, err := formatJSON(usage)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed jsonUsageOutput
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if parsed.Session == nil || parsed.Session.Utilization != 42 {
		t.Errorf("expected session utilization 42, got %+v", parsed.Session)
	}
	if parsed.Weekly != nil {
		t.Error("expected weekly to be nil")
	}
}

func TestFormatJSON_OnlyWeekly(t *testing.T) {
	usage := usageResponse{
		SevenDay: &usageBucket{Utilization: 77},
	}

	got, err := formatJSON(usage)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed jsonUsageOutput
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if parsed.Session != nil {
		t.Error("expected session to be nil")
	}
	if parsed.Weekly == nil || parsed.Weekly.Utilization != 77 {
		t.Errorf("expected weekly utilization 77, got %+v", parsed.Weekly)
	}
}

func TestFormatReset(t *testing.T) {
	tests := []struct {
		name   string
		raw    *string
		layout string
		empty  bool
	}{
		{"nil input", nil, "3:04pm", true},
		{"valid RFC3339", ptr("2026-02-25T02:00:00.000000+00:00"), "3:04pm", false},
		{"invalid timestamp", ptr("not-a-time"), "3:04pm", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatReset(tt.raw, tt.layout)
			if tt.empty && got != "" {
				t.Errorf("expected empty string, got %q", got)
			}
			if !tt.empty && got == "" {
				t.Error("expected non-empty string, got empty")
			}
		})
	}
}

func TestFormatCustom(t *testing.T) {
	reset5h := "2026-02-25T02:00:00.000000+00:00"
	reset7d := "2026-03-03T07:00:00.000000+00:00"

	fullUsage := usageResponse{
		FiveHour: &usageBucket{Utilization: 45, ResetsAt: &reset5h},
		SevenDay: &usageBucket{Utilization: 13, ResetsAt: &reset7d},
	}

	tests := []struct {
		name      string
		format    string
		usage     usageResponse
		plan      string
		remaining bool
		want      string
		checkFn   func(string) bool // for timezone-dependent output
	}{
		{
			name:   "plan verb",
			format: "%p",
			usage:  fullUsage,
			plan:   "pro",
			want:   "Pro",
		},
		{
			name:   "session utilization verb",
			format: "%s",
			usage:  fullUsage,
			plan:   "pro",
			want:   "45%",
		},
		{
			name:   "session reset verb",
			format: "%S",
			usage:  fullUsage,
			plan:   "pro",
			checkFn: func(s string) bool {
				return s != "" // timezone-dependent, just check non-empty
			},
		},
		{
			name:   "weekly utilization verb",
			format: "%w",
			usage:  fullUsage,
			plan:   "pro",
			want:   "13%",
		},
		{
			name:   "weekly reset verb",
			format: "%W",
			usage:  fullUsage,
			plan:   "pro",
			checkFn: func(s string) bool {
				return s != ""
			},
		},
		{
			name:   "literal percent",
			format: "%%",
			usage:  fullUsage,
			plan:   "pro",
			want:   "%",
		},
		{
			name:   "nil session bucket - session verbs empty",
			format: "[%s][%S]",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 10}},
			plan:   "pro",
			want:   "[][]",
		},
		{
			name:   "nil weekly bucket - weekly verbs empty",
			format: "[%w][%W]",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 10}},
			plan:   "pro",
			want:   "[][]",
		},
		{
			name:      "remaining mode session",
			format:    "%s",
			usage:     usageResponse{FiveHour: &usageBucket{Utilization: 45}},
			plan:      "pro",
			remaining: true,
			want:      "55% left",
		},
		{
			name:      "remaining mode weekly",
			format:    "%w",
			usage:     usageResponse{SevenDay: &usageBucket{Utilization: 13}},
			plan:      "pro",
			remaining: true,
			want:      "87% left",
		},
		{
			name:   "no reset time - session reset empty",
			format: "[%S]",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45}},
			plan:   "pro",
			want:   "[]",
		},
		{
			name:   "no reset time - weekly reset empty",
			format: "[%W]",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 13}},
			plan:   "pro",
			want:   "[]",
		},
		{
			name:   "mixed text and verbs",
			format: "%p: %s used",
			usage:  fullUsage,
			plan:   "pro",
			want:   "Pro: 45% used",
		},
		{
			name:   "unknown verb passes through",
			format: "%x",
			usage:  fullUsage,
			plan:   "pro",
			want:   "%x",
		},
		{
			name:   "no verbs returns literal text",
			format: "hello world",
			usage:  fullUsage,
			plan:   "pro",
			want:   "hello world",
		},
		{
			name:   "empty plan becomes Unknown",
			format: "%p",
			usage:  fullUsage,
			plan:   "",
			want:   "Unknown",
		},
		{
			name:   "all nil buckets",
			format: "%p %s %S %w %W",
			usage:  usageResponse{},
			plan:   "max",
			want:   "Max    ",
		},
		{
			name:   "trailing percent sign",
			format: "test%",
			usage:  fullUsage,
			plan:   "pro",
			want:   "test%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCustom(tt.format, tt.usage, tt.plan, tt.remaining)
			if tt.checkFn != nil {
				if !tt.checkFn(got) {
					t.Errorf("formatCustom(%q) = %q, check failed", tt.format, got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("formatCustom(%q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

func TestFormatDisplay_AlwaysWeeklyOverridesThreshold(t *testing.T) {
	// With alwaysWeekly, even low weekly usage should appear
	usage := usageResponse{
		FiveHour: &usageBucket{Utilization: 10},
		SevenDay: &usageBucket{Utilization: 5},
	}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80, alwaysWeekly: true}

	got := formatDisplay(usage, "max", opts)
	want := "Claude Max 10%: Weekly: 5%"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatDisplay_WeeklyExactThreshold(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{Utilization: 50},
		SevenDay: &usageBucket{Utilization: 80},
	}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80}

	got := formatDisplay(usage, "max", opts)
	want := "Claude Max 50% [week: 80%]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripTermCodes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text unchanged", "hello world", "hello world"},
		{"CSI color code", "\x1b[31mred\x1b[0m", "red"},
		{"OSC title stripped", "\x1b]0;title\x1b\\", ""},
		{"OSC palette stripped", "\x1b]4;0;#272822\x1b\\", ""},
		{"text around OSC", "before\x1b]0;title\x1b\\after", "beforeafter"},
		{"mixed codes and text", "\x1b[1mClaude\x1b[0m Pro 45%", "Claude Pro 45%"},
		{"empty string", "", ""},
		{"no codes", "Claude Pro 45%", "Claude Pro 45%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripTermCodes(tt.input)
			if got != tt.want {
				t.Errorf("stripTermCodes(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripTermCodes_CharsetAndTwoChar(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"charset select", "\x1b(Btext", "text"},
		{"two-char escape", "\x1bMtext", "text"},
		{"BEL-terminated OSC", "\x1b]0;title\x07text", "text"},
		{"multiple mixed sequences", "\x1b[1m\x1b]0;t\x07\x1b(Bhello\x1b[0m", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripTermCodes(tt.input)
			if got != tt.want {
				t.Errorf("stripTermCodes(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRefreshToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	oldEndpoint := tokenEndpoint
	tokenEndpoint = srv.URL
	defer func() { tokenEndpoint = oldEndpoint }()

	dir := t.TempDir()
	credsPath := filepath.Join(dir, "creds.json")

	creds := &credentials{
		ClaudeAiOauth: oauthCredentials{
			AccessToken:  "old-access",
			RefreshToken: "old-refresh",
		},
	}

	if err := refreshToken(creds, credsPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if creds.ClaudeAiOauth.AccessToken != "new-access" {
		t.Errorf("access token = %q, want %q", creds.ClaudeAiOauth.AccessToken, "new-access")
	}
	if creds.ClaudeAiOauth.RefreshToken != "new-refresh" {
		t.Errorf("refresh token = %q, want %q", creds.ClaudeAiOauth.RefreshToken, "new-refresh")
	}
	if creds.ClaudeAiOauth.ExpiresAt == 0 {
		t.Error("expected ExpiresAt to be set")
	}

	data, err := os.ReadFile(credsPath)
	if err != nil {
		t.Fatalf("reading saved credentials: %v", err)
	}
	var saved credentials
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("parsing saved credentials: %v", err)
	}
	if saved.ClaudeAiOauth.AccessToken != "new-access" {
		t.Errorf("saved access token = %q, want %q", saved.ClaudeAiOauth.AccessToken, "new-access")
	}
}

func TestRefreshToken_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	oldEndpoint := tokenEndpoint
	tokenEndpoint = srv.URL
	defer func() { tokenEndpoint = oldEndpoint }()

	creds := &credentials{
		ClaudeAiOauth: oauthCredentials{
			RefreshToken: "some-token",
		},
	}

	err := refreshToken(creds, "/dev/null")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); !contains(got, "refresh failed") {
		t.Errorf("error = %q, want it to contain %q", got, "refresh failed")
	}
}

func TestRefreshToken_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	oldEndpoint := tokenEndpoint
	tokenEndpoint = srv.URL
	defer func() { tokenEndpoint = oldEndpoint }()

	creds := &credentials{
		ClaudeAiOauth: oauthCredentials{
			RefreshToken: "some-token",
		},
	}

	err := refreshToken(creds, "/dev/null")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); !contains(got, "parsing refresh response") {
		t.Errorf("error = %q, want it to contain %q", got, "parsing refresh response")
	}
}

func TestRefreshToken_ConnectionError(t *testing.T) {
	oldEndpoint := tokenEndpoint
	tokenEndpoint = "http://127.0.0.1:1" // nothing listening
	defer func() { tokenEndpoint = oldEndpoint }()

	creds := &credentials{
		ClaudeAiOauth: oauthCredentials{
			RefreshToken: "some-token",
		},
	}

	err := refreshToken(creds, "/dev/null")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); !contains(got, "refresh request failed") {
		t.Errorf("error = %q, want it to contain %q", got, "refresh request failed")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestFormatDisplay_SessionExactThreshold(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{
			Utilization: 90,
			ResetsAt:    ptr("2026-02-25T02:00:00.000000+00:00"),
		},
	}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80}

	got := formatDisplay(usage, "max", opts)
	// 90 >= 90, so reset time should be shown
	if got == "Claude Max 90%" {
		t.Error("expected reset time at exact threshold")
	}
}
