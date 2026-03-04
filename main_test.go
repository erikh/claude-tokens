package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
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

	defaultOpts := displayOptions{sessionPct: 90, weeklyPct: 80}

	tests := []struct {
		name    string
		format  string
		usage   usageResponse
		plan    string
		opts    displayOptions
		want    string
		checkFn func(string) bool // for timezone-dependent output
	}{
		{
			name:   "plan verb",
			format: "%p",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "Pro",
		},
		{
			name:   "session utilization verb",
			format: "%s",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "45%",
		},
		{
			name:   "session reset verb",
			format: "%S",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			checkFn: func(s string) bool {
				return s != "" // timezone-dependent, just check non-empty
			},
		},
		{
			name:   "weekly utilization verb",
			format: "%w",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "13%",
		},
		{
			name:   "weekly reset verb",
			format: "%W",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			checkFn: func(s string) bool {
				return s != ""
			},
		},
		{
			name:   "literal percent",
			format: "%%",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "%",
		},
		{
			name:   "nil session bucket - session verbs empty",
			format: "[%s][%S]",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 10}},
			plan:   "pro",
			opts:   defaultOpts,
			want:   "[][]",
		},
		{
			name:   "nil weekly bucket - weekly verbs empty",
			format: "[%w][%W]",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 10}},
			plan:   "pro",
			opts:   defaultOpts,
			want:   "[][]",
		},
		{
			name:   "remaining mode session",
			format: "%s",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80, remaining: true},
			want:   "55% left",
		},
		{
			name:   "remaining mode weekly",
			format: "%w",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 13}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80, remaining: true},
			want:   "87% left",
		},
		{
			name:   "no reset time - session reset empty",
			format: "[%S]",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45}},
			plan:   "pro",
			opts:   defaultOpts,
			want:   "[]",
		},
		{
			name:   "no reset time - weekly reset empty",
			format: "[%W]",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 13}},
			plan:   "pro",
			opts:   defaultOpts,
			want:   "[]",
		},
		{
			name:   "mixed text and verbs",
			format: "%p: %s used",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "Pro: 45% used",
		},
		{
			name:   "unknown verb passes through",
			format: "%x",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "%x",
		},
		{
			name:   "no verbs returns literal text",
			format: "hello world",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "hello world",
		},
		{
			name:   "empty plan becomes Unknown",
			format: "%p",
			usage:  fullUsage,
			plan:   "",
			opts:   defaultOpts,
			want:   "Unknown",
		},
		{
			name:   "all nil buckets",
			format: "%p %s %S %w %W",
			usage:  usageResponse{},
			plan:   "max",
			opts:   defaultOpts,
			want:   "Max    ",
		},
		{
			name:   "trailing percent sign",
			format: "test%",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "test%",
		},
		// %t - bare, no modifier: no enclosing characters, just the reset time
		{
			name:   "bare session reset - above threshold raw time",
			format: "%t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 95, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return s != "" && s[0] != '(' && s[0] != '['
			},
		},
		{
			name:   "bare session reset - at exact threshold raw time",
			format: "%t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 90, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return s != "" && s[0] != '(' && s[0] != '['
			},
		},
		{
			name:   "bare session reset - below threshold empty",
			format: "<%t>",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "<>",
		},
		{
			name:   "bare session reset - nil bucket empty",
			format: "<%t>",
			usage:  usageResponse{},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "<>",
		},
		{
			name:   "bare session reset - no reset time empty",
			format: "<%t>",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 95}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "<>",
		},
		// %T - bare weekly
		{
			name:   "bare weekly reset - above threshold raw time",
			format: "%T",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 85, ResetsAt: &reset7d}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return s != "" && s[0] != '(' && s[0] != '['
			},
		},
		{
			name:   "bare weekly reset - below threshold empty",
			format: "<%T>",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 50, ResetsAt: &reset7d}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "<>",
		},
		// %(t - parens modifier: enclosed in parentheses
		{
			name:   "parens session reset - above threshold enclosed in parens",
			format: "%(t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 95, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return len(s) > len("(resets )") &&
					s[:8] == "(resets " && s[len(s)-1] == ')'
			},
		},
		{
			name:   "parens session reset - below threshold empty",
			format: "<%(t>",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "<>",
		},
		{
			name:   "parens session reset - nil bucket empty",
			format: "<%(t>",
			usage:  usageResponse{},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "<>",
		},
		{
			name:   "parens session reset - no reset time empty",
			format: "<%(t>",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 95}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "<>",
		},
		// %(T - parens weekly
		{
			name:   "parens weekly reset - above threshold enclosed in parens",
			format: "%(T",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 85, ResetsAt: &reset7d}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return len(s) > len("(resets )") &&
					s[:8] == "(resets " && s[len(s)-1] == ')'
			},
		},
		{
			name:   "parens weekly reset - below threshold empty",
			format: "<%(T>",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 50, ResetsAt: &reset7d}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "<>",
		},
		// %[t - brackets modifier: enclosed in brackets
		{
			name:   "brackets session reset - above threshold enclosed in brackets",
			format: "%[t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 95, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return len(s) > len("[resets ]") &&
					s[:8] == "[resets " && s[len(s)-1] == ']'
			},
		},
		{
			name:   "brackets session reset - below threshold empty",
			format: "<%[t>",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "<>",
		},
		{
			name:   "brackets session reset - nil bucket empty",
			format: "<%[t>",
			usage:  usageResponse{},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "<>",
		},
		{
			name:   "brackets session reset - no reset time empty",
			format: "<%[t>",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 95}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "<>",
		},
		// %[T - brackets weekly
		{
			name:   "brackets weekly reset - above threshold enclosed in brackets",
			format: "%[T",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 85, ResetsAt: &reset7d}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return len(s) > len("[resets ]") &&
					s[:8] == "[resets " && s[len(s)-1] == ']'
			},
		},
		{
			name:   "brackets weekly reset - below threshold empty",
			format: "<%[T>",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 50, ResetsAt: &reset7d}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "<>",
		},
		// %[t / %(t with no reset time - nothing
		{
			name:   "brackets session - above threshold no reset time produces nothing",
			format: "%s%[t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 95}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "95%",
		},
		{
			name:   "parens session - above threshold no reset time produces nothing",
			format: "%s%(t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 95}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "95%",
		},
		// in context: %s %(t and %s %[t
		{
			name:   "parens in context - above threshold",
			format: "%s %(t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 95, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return len(s) > len("95% (resets )") &&
					s[:12] == "95% (resets " && s[len(s)-1] == ')'
			},
		},
		{
			name:   "parens in context - below threshold clean",
			format: "%s %(t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "45% ",
		},
		{
			name:   "brackets in context - above threshold",
			format: "%s %[t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 95, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return len(s) > len("95% [resets ]") &&
					s[:12] == "95% [resets " && s[len(s)-1] == ']'
			},
		},
		{
			name:   "brackets in context - below threshold clean",
			format: "%s %[t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "45% ",
		},
		// combined: session parens, weekly brackets
		{
			name:   "mixed modifiers - session parens weekly brackets both above",
			format: "%s %(t %w %[T",
			usage: usageResponse{
				FiveHour: &usageBucket{Utilization: 95, ResetsAt: &reset5h},
				SevenDay: &usageBucket{Utilization: 85, ResetsAt: &reset7d},
			},
			plan: "pro",
			opts: displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return contains(s, "95% (resets ") &&
					contains(s, ") ") &&
					s[len(s)-1] == ']'
			},
		},
		{
			name:   "mixed modifiers - session above weekly below",
			format: "%s %(t %w %[T",
			usage: usageResponse{
				FiveHour: &usageBucket{Utilization: 95, ResetsAt: &reset5h},
				SevenDay: &usageBucket{Utilization: 50, ResetsAt: &reset7d},
			},
			plan: "pro",
			opts: displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return contains(s, "95% (resets ") &&
					contains(s, ") ") &&
					contains(s, "50% ")
			},
		},
		// unknown verb with ( or [ modifier passes through
		{
			name:   "unknown verb with paren modifier passes through",
			format: "%(x",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "%(x",
		},
		{
			name:   "unknown verb with bracket modifier passes through",
			format: "%[x",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "%[x",
		},
		// arbitrary delimiter characters
		{
			name:   "dash modifier repeats as delimiter",
			format: "%-t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 95, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return len(s) > len("-resets -") &&
					s[0] == '-' && s[:8] == "-resets " && s[len(s)-1] == '-'
			},
		},
		{
			name:   "dash modifier below threshold empty",
			format: "<%-t>",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "<>",
		},
		{
			name:   "pipe modifier repeats as delimiter",
			format: "%|t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 95, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return len(s) > len("|resets |") &&
					s[0] == '|' && s[:8] == "|resets " && s[len(s)-1] == '|'
			},
		},
		{
			name:   "asterisk modifier repeats as delimiter",
			format: "%*t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 95, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return len(s) > len("*resets *") &&
					s[0] == '*' && s[:8] == "*resets " && s[len(s)-1] == '*'
			},
		},
		{
			name:   "exclamation modifier repeats as delimiter",
			format: "%!t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 95, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return len(s) > len("!resets !") &&
					s[0] == '!' && s[:8] == "!resets " && s[len(s)-1] == '!'
			},
		},
		{
			name:   "dash modifier on weekly reset",
			format: "%-T",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 85, ResetsAt: &reset7d}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return len(s) > len("-resets -") &&
					s[0] == '-' && s[:8] == "-resets " && s[len(s)-1] == '-'
			},
		},
		{
			name:   "pipe modifier on weekly reset below threshold empty",
			format: "<%|T>",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 50, ResetsAt: &reset7d}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "<>",
		},
		{
			name:   "dash modifier in context above threshold",
			format: "%s %-t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 95, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			checkFn: func(s string) bool {
				return len(s) > len("95% -resets -") &&
					s[:12] == "95% -resets " && s[len(s)-1] == '-'
			},
		},
		{
			name:   "dash modifier in context below threshold clean",
			format: "%s %-t",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   displayOptions{sessionPct: 90, weeklyPct: 80},
			want:   "45% ",
		},
		// %% is literal percent, not a modifier — %%t should produce "%t"
		{
			name:   "double percent then t is literal percent then literal t",
			format: "%%t",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "%t",
		},
		{
			name:   "double percent then T is literal percent then literal T",
			format: "%%T",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "%T",
		},
		// unknown verb with arbitrary modifier passes through
		{
			name:   "unknown verb with dash modifier passes through",
			format: "%-x",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "%-x",
		},
		{
			name:   "unknown verb with pipe modifier passes through",
			format: "%|x",
			usage:  fullUsage,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "%|x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCustom(tt.format, tt.usage, tt.plan, tt.opts)
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

func TestStripCR(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no CR", "hello world", "hello world"},
		{"single CR", "hello\rworld", "helloworld"},
		{"CR at start", "\rhello", "hello"},
		{"CR at end", "hello\r", "hello"},
		{"multiple CR", "a\rb\rc\r", "abc"},
		{"CRLF stripped to LF", "line1\r\nline2", "line1\nline2"},
		{"empty string", "", ""},
		{"only CR", "\r\r\r", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripCR(tt.input)
			if got != tt.want {
				t.Errorf("stripCR(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestReverseModifier(t *testing.T) {
	tests := []struct {
		name string
		mod  string
		want string
	}{
		// single ASCII characters
		{"single dash", "-", "-"},
		{"single pipe", "|", "|"},
		{"single paren", "(", ")"},
		{"single bracket", "[", "]"},
		{"single angle", "<", ">"},
		{"close paren mirrors", ")", "("},
		{"close bracket mirrors", "]", "["},
		{"close angle mirrors", ">", "<"},
		// multi-char ASCII, no pairing
		{"double dash", "--", "--"},
		{"triple star", "***", "***"},
		{"arrow =>", "=>", "<="},
		{"arrow <=", "<=", "=>"},
		// multi-char ending in paired ASCII
		{"dash-eq-bracket", "-=[", "]=-"},
		{"dash-eq-paren", "-=(", ")=-"},
		{"dash-eq-angle", "-=<", ">=-"},
		{"lt-lt-bracket", "<<[", "]>>"},
		{"lt-lt-paren", "<<(", ")>>"},
		{"hash-pipe-bracket", "#|[", "]|#"},
		// Unicode arrows
		{"left arrow", "←", "→"},
		{"right arrow", "→", "←"},
		{"double left arrow", "⇐", "⇒"},
		{"double right arrow", "⇒", "⇐"},
		{"long left arrow", "⟵", "⟶"},
		{"long right arrow", "⟶", "⟵"},
		{"long double left", "⟸", "⟹"},
		{"long double right", "⟹", "⟸"},
		// Unicode triangles
		{"left triangle filled", "◀", "▶"},
		{"right triangle filled", "▶", "◀"},
		{"left triangle outline", "◁", "▷"},
		{"right triangle outline", "▷", "◁"},
		{"left small filled", "◂", "▸"},
		{"right small filled", "▸", "◂"},
		{"left small outline", "◃", "▹"},
		{"right small outline", "▹", "◃"},
		{"up triangle", "▲", "▼"},
		{"down triangle", "▼", "▲"},
		{"up outline", "△", "▽"},
		{"down outline", "▽", "△"},
		// Unicode angle brackets and chevrons
		{"math angle left", "⟨", "⟩"},
		{"math angle right", "⟩", "⟨"},
		{"guillemet left", "«", "»"},
		{"guillemet right", "»", "«"},
		{"single guillemet left", "‹", "›"},
		{"single guillemet right", "›", "‹"},
		{"ornament left paren", "❨", "❩"},
		{"ornament right paren", "❩", "❨"},
		{"heavy left angle", "❮", "❯"},
		{"heavy right angle", "❯", "❮"},
		{"heavy left bracket", "❰", "❱"},
		{"heavy right bracket", "❱", "❰"},
		// floor/ceiling
		{"left ceiling", "⌈", "⌉"},
		{"right ceiling", "⌉", "⌈"},
		{"left floor", "⌊", "⌋"},
		{"right floor", "⌋", "⌊"},
		// multi-char Unicode combinations
		{"arrows with dash", "-→", "←-"},
		{"guillemets with eq", "=«", "»="},
		{"triangles with bracket", "◀[", "]▶"},
		{"dash-arrow-paren", "-→(", ")←-"},
		{"complex mixed", "«-=[", "]=-»"},
		// empty
		{"empty string", "", ""},
		// non-paired characters stay the same
		{"at sign", "@", "@"},
		{"tilde", "~", "~"},
		{"hash", "#", "#"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reverseModifier(tt.mod)
			if got != tt.want {
				t.Errorf("reverseModifier(%q) = %q, want %q", tt.mod, got, tt.want)
			}
		})
	}
}

func TestFormatCustom_MultiCharModifier(t *testing.T) {
	reset5h := "2026-02-25T02:00:00.000000+00:00"
	reset7d := "2026-03-03T07:00:00.000000+00:00"

	aboveSession := usageResponse{FiveHour: &usageBucket{Utilization: 95, ResetsAt: &reset5h}}
	belowSession := usageResponse{FiveHour: &usageBucket{Utilization: 45, ResetsAt: &reset5h}}
	aboveWeekly := usageResponse{SevenDay: &usageBucket{Utilization: 85, ResetsAt: &reset7d}}

	defaultOpts := displayOptions{sessionPct: 90, weeklyPct: 80}

	tests := []struct {
		name    string
		format  string
		usage   usageResponse
		plan    string
		opts    displayOptions
		want    string
		checkFn func(string) bool
	}{
		// multi-char modifier on t/T
		{
			name:   "double dash session above threshold",
			format: "%--t",
			usage:  aboveSession,
			plan:   "pro",
			opts:   defaultOpts,
			checkFn: func(s string) bool {
				return len(s) > 4 && s[:2] == "--" && s[:9] == "--resets " && s[len(s)-2:] == "--"
			},
		},
		{
			name:   "double dash session below threshold empty",
			format: "<%--t>",
			usage:  belowSession,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "<>",
		},
		{
			name:   "arrow modifier on session",
			format: "%=>t",
			usage:  aboveSession,
			plan:   "pro",
			opts:   defaultOpts,
			checkFn: func(s string) bool {
				return len(s) > 4 && s[:2] == "=>" && s[:9] == "=>resets " && s[len(s)-2:] == "<="
			},
		},
		{
			name:   "dash-eq-bracket modifier",
			format: "%-=[t",
			usage:  aboveSession,
			plan:   "pro",
			opts:   defaultOpts,
			checkFn: func(s string) bool {
				return len(s) > 6 && s[:3] == "-=[" && s[:10] == "-=[resets " && s[len(s)-3:] == "]=-"
			},
		},
		{
			name:   "dash-eq-paren modifier",
			format: "%-=(t",
			usage:  aboveSession,
			plan:   "pro",
			opts:   defaultOpts,
			checkFn: func(s string) bool {
				return len(s) > 6 && s[:3] == "-=(" && s[:10] == "-=(resets " && s[len(s)-3:] == ")=-"
			},
		},
		{
			name:   "lt-lt-bracket modifier",
			format: "%<<[t",
			usage:  aboveSession,
			plan:   "pro",
			opts:   defaultOpts,
			checkFn: func(s string) bool {
				return len(s) > 6 && s[:3] == "<<[" && s[:10] == "<<[resets " && s[len(s)-3:] == "]>>"
			},
		},
		{
			name:   "lt-lt-paren modifier",
			format: "%<<(t",
			usage:  aboveSession,
			plan:   "pro",
			opts:   defaultOpts,
			checkFn: func(s string) bool {
				return len(s) > 6 && s[:3] == "<<(" && s[:10] == "<<(resets " && s[len(s)-3:] == ")>>"
			},
		},
		{
			name:   "triple star modifier",
			format: "%***t",
			usage:  aboveSession,
			plan:   "pro",
			opts:   defaultOpts,
			checkFn: func(s string) bool {
				return len(s) > 6 && s[:3] == "***" && s[:10] == "***resets " && s[len(s)-3:] == "***"
			},
		},
		{
			name:   "multi-char modifier on weekly",
			format: "%--T",
			usage:  aboveWeekly,
			plan:   "pro",
			opts:   defaultOpts,
			checkFn: func(s string) bool {
				return len(s) > 4 && s[:2] == "--" && s[:9] == "--resets " && s[len(s)-2:] == "--"
			},
		},
		{
			name:   "dash-eq-bracket weekly below threshold empty",
			format: "<%-=[T>",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 50, ResetsAt: &reset7d}},
			plan:   "pro",
			opts:   defaultOpts,
			want:   "<>",
		},
		// modifiers on all verbs (not just t/T)
		{
			name:   "modifier on plan verb",
			format: "%-=p",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45}},
			plan:   "pro",
			opts:   defaultOpts,
			want:   "-=Pro=-",
		},
		{
			name:   "modifier on session pct verb",
			format: "%«s",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45}},
			plan:   "pro",
			opts:   defaultOpts,
			want:   "«45%»",
		},
		{
			name:   "modifier on session reset verb",
			format: "%--S",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45, ResetsAt: &reset5h}},
			plan:   "pro",
			opts:   defaultOpts,
			checkFn: func(s string) bool {
				return len(s) > 4 && s[:2] == "--" && s[len(s)-2:] == "--"
			},
		},
		{
			name:   "modifier on weekly pct verb",
			format: "%(w",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 13}},
			plan:   "pro",
			opts:   defaultOpts,
			want:   "(13%)",
		},
		{
			name:   "modifier on weekly reset verb",
			format: "%-=W",
			usage:  usageResponse{SevenDay: &usageBucket{Utilization: 13, ResetsAt: &reset7d}},
			plan:   "pro",
			opts:   defaultOpts,
			checkFn: func(s string) bool {
				return len(s) > 4 && s[:2] == "-=" && s[len(s)-2:] == "=-"
			},
		},
		{
			name:   "modifier on nil bucket produces empty",
			format: "%-=s",
			usage:  usageResponse{},
			plan:   "pro",
			opts:   defaultOpts,
			want:   "",
		},
		// Unicode modifiers
		{
			name:   "left arrow modifier",
			format: "%←t",
			usage:  aboveSession,
			plan:   "pro",
			opts:   defaultOpts,
			checkFn: func(s string) bool {
				runes := []rune(s)
				return len(runes) > 2 && runes[0] == '←' &&
					runes[len(runes)-1] == '→'
			},
		},
		{
			name:   "guillemet modifier on plan",
			format: "%«p",
			usage:  aboveSession,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "«Pro»",
		},
		{
			name:   "heavy angle modifier on session",
			format: "%❮s",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45}},
			plan:   "pro",
			opts:   defaultOpts,
			want:   "❮45%❯",
		},
		{
			name:   "triangle modifier",
			format: "%◀s",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45}},
			plan:   "pro",
			opts:   defaultOpts,
			want:   "◀45%▶",
		},
		{
			name:   "math angle bracket modifier",
			format: "%⟨s",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45}},
			plan:   "pro",
			opts:   defaultOpts,
			want:   "⟨45%⟩",
		},
		{
			name:   "double arrow modifier",
			format: "%⇐s",
			usage:  usageResponse{FiveHour: &usageBucket{Utilization: 45}},
			plan:   "pro",
			opts:   defaultOpts,
			want:   "⇐45%⇒",
		},
		{
			name:   "mixed unicode multi-char",
			format: "%«-=[t",
			usage:  aboveSession,
			plan:   "pro",
			opts:   defaultOpts,
			checkFn: func(s string) bool {
				runes := []rune(s)
				// open: «-=[  close: ]=->»  (mirror each, reverse)
				return len(runes) > 8 &&
					string(runes[:4]) == "«-=[" &&
					string(runes[len(runes)-4:]) == "]=-»"
			},
		},
		{
			name:   "dash-arrow-paren modifier",
			format: "%-→(t",
			usage:  aboveSession,
			plan:   "pro",
			opts:   defaultOpts,
			checkFn: func(s string) bool {
				runes := []rune(s)
				return len(runes) > 6 &&
					string(runes[:3]) == "-→(" &&
					string(runes[len(runes)-3:]) == ")←-"
			},
		},
		// edge cases
		{
			name:   "no verb found outputs literally",
			format: "%abc",
			usage:  aboveSession,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "%abc",
		},
		{
			name:   "brace reserved for color not modifier",
			format: "%{red}text",
			usage:  aboveSession,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "\x1b[31mtext",
		},
		{
			name:   "multi-char unknown verb passthrough",
			format: "%--x",
			usage:  aboveSession,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "%--x",
		},
		{
			name:   "multi-char unknown verb with bracket passthrough",
			format: "%-=[x",
			usage:  aboveSession,
			plan:   "pro",
			opts:   defaultOpts,
			want:   "%-=[x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCustom(tt.format, tt.usage, tt.plan, tt.opts)
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

func TestColorSpec(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want string
	}{
		{"foreground red", "red", "\x1b[31m"},
		{"foreground green", "green", "\x1b[32m"},
		{"foreground blue", "blue", "\x1b[34m"},
		{"foreground white", "white", "\x1b[37m"},
		{"foreground black", "black", "\x1b[30m"},
		{"foreground yellow", "yellow", "\x1b[33m"},
		{"foreground magenta", "magenta", "\x1b[35m"},
		{"foreground cyan", "cyan", "\x1b[36m"},
		{"hi-intensity red", "hired", "\x1b[91m"},
		{"hi-intensity green", "higreen", "\x1b[92m"},
		{"hi-intensity blue", "hiblue", "\x1b[94m"},
		{"fg with background", "red on white", "\x1b[31;47m"},
		{"fg with bg green", "blue on green", "\x1b[34;42m"},
		{"fg with bold", "red with bold", "\x1b[31;1m"},
		{"fg with underline", "green with underline", "\x1b[32;4m"},
		{"fg with italic", "cyan with italic", "\x1b[36;3m"},
		{"fg with faint", "white with faint", "\x1b[37;2m"},
		{"fg with crossedout", "red with crossedout", "\x1b[31;9m"},
		{"fg on bg with bold", "red on white with bold", "\x1b[31;47;1m"},
		{"fg on bg with multiple attrs", "green on black with bold underline", "\x1b[32;40;1;4m"},
		{"reset only", "white with reset", "\x1b[37;0m"},
		{"empty spec", "", ""},
		{"unknown color", "notacolor", ""},
		{"unknown bg ignored", "red on notabg", "\x1b[31m"},
		{"unknown attr ignored", "red with notanattr", "\x1b[31m"},
		{"reversevideo", "white with reversevideo", "\x1b[37;7m"},
		{"concealed", "red with concealed", "\x1b[31;8m"},
		{"blinkslow", "red with blinkslow", "\x1b[31;5m"},
		{"blinkrapid", "red with blinkrapid", "\x1b[31;6m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := colorSpec(tt.spec)
			if got != tt.want {
				t.Errorf("colorSpec(%q) = %q, want %q", tt.spec, got, tt.want)
			}
		})
	}
}

func TestFormatCustom_ColorVerb(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{Utilization: 45},
	}
	opts := displayOptions{sessionPct: 90, weeklyPct: 80}

	tests := []struct {
		name   string
		format string
		want   string
	}{
		{
			name:   "color before text",
			format: "%{red}hello",
			want:   "\x1b[31mhello",
		},
		{
			name:   "color with reset",
			format: "%{red}hello%{white with reset}",
			want:   "\x1b[31mhello\x1b[37;0m",
		},
		{
			name:   "color around format verb",
			format: "%{green}%s%{white with reset}",
			want:   "\x1b[32m45%\x1b[37;0m",
		},
		{
			name:   "bold color",
			format: "%{red with bold}%p%{white with reset}",
			want:   "\x1b[31;1mPro\x1b[37;0m",
		},
		{
			name:   "color with background",
			format: "%{white on red}alert%{white with reset}",
			want:   "\x1b[37;41malert\x1b[37;0m",
		},
		{
			name:   "multiple colors in sequence",
			format: "%{red}R%{green}G%{blue}B",
			want:   "\x1b[31mR\x1b[32mG\x1b[34mB",
		},
		{
			name:   "no closing brace passes through",
			format: "%{red hello",
			want:   "%{red hello",
		},
		{
			name:   "empty color spec",
			format: "%{}text",
			want:   "text",
		},
		{
			name:   "color mixed with other verbs",
			format: "%{cyan}%p: %s%{white with reset}",
			want:   "\x1b[36mPro: 45%\x1b[37;0m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCustom(tt.format, usage, "pro", opts)
			if got != tt.want {
				t.Errorf("formatCustom(%q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

func TestFormatCustom_PromptMode(t *testing.T) {
	usage := usageResponse{
		FiveHour: &usageBucket{Utilization: 45},
		SevenDay: &usageBucket{Utilization: 13},
	}

	tests := []struct {
		name       string
		format     string
		promptMode string
		want       string
	}{
		// zsh mode
		{
			name:       "zsh: single color wrapped",
			format:     "%{red}hello",
			promptMode: "zsh",
			want:       "%{\x1b[31m%}hello",
		},
		{
			name:       "zsh: color with reset wrapped",
			format:     "%{red}hello%{white with reset}",
			promptMode: "zsh",
			want:       "%{\x1b[31m%}hello%{\x1b[37;0m%}",
		},
		{
			name:       "zsh: color around format verb",
			format:     "%{green}%s%{white with reset}",
			promptMode: "zsh",
			want:       "%{\x1b[32m%}45%%%{\x1b[37;0m%}",
		},
		{
			name:       "zsh: multiple colors",
			format:     "%{red}R%{green}G%{blue}B",
			promptMode: "zsh",
			want:       "%{\x1b[31m%}R%{\x1b[32m%}G%{\x1b[34m%}B",
		},
		{
			name:       "zsh: color with background wrapped",
			format:     "%{white on red}alert%{white with reset}",
			promptMode: "zsh",
			want:       "%{\x1b[37;41m%}alert%{\x1b[37;0m%}",
		},
		// bash mode
		{
			name:       "bash: single color wrapped",
			format:     "%{red}hello",
			promptMode: "bash",
			want:       "\001\x1b[31m\002hello",
		},
		{
			name:       "bash: color with reset wrapped",
			format:     "%{red}hello%{white with reset}",
			promptMode: "bash",
			want:       "\001\x1b[31m\002hello\001\x1b[37;0m\002",
		},
		{
			name:       "bash: color around format verb",
			format:     "%{green}%s%{white with reset}",
			promptMode: "bash",
			want:       "\001\x1b[32m\002" + "45%" + "\001\x1b[37;0m\002",
		},
		{
			name:       "bash: multiple colors",
			format:     "%{red}R%{green}G%{blue}B",
			promptMode: "bash",
			want:       "\001\x1b[31m\002R\001\x1b[32m\002G\001\x1b[34m\002B",
		},
		{
			name:       "bash: color with background wrapped",
			format:     "%{white on red}alert%{white with reset}",
			promptMode: "bash",
			want:       "\001\x1b[37;41m\002alert\001\x1b[37;0m\002",
		},
		// shared: empty/unknown specs not wrapped in either mode
		{
			name:       "zsh: empty color spec not wrapped",
			format:     "%{}text",
			promptMode: "zsh",
			want:       "text",
		},
		{
			name:       "bash: empty color spec not wrapped",
			format:     "%{}text",
			promptMode: "bash",
			want:       "text",
		},
		{
			name:       "zsh: unknown color not wrapped",
			format:     "%{notacolor}text",
			promptMode: "zsh",
			want:       "text",
		},
		{
			name:       "bash: unknown color not wrapped",
			format:     "%{notacolor}text",
			promptMode: "bash",
			want:       "text",
		},
		// non-color verbs and borders unaffected
		{
			name:       "zsh: non-color verbs escaped",
			format:     "%p: %s %w",
			promptMode: "zsh",
			want:       "Pro: 45%% 13%%",
		},
		{
			name:       "bash: non-color verbs unaffected",
			format:     "%p: %s %w",
			promptMode: "bash",
			want:       "Pro: 45% 13%",
		},
		{
			name:       "zsh: borders with pct escaped",
			format:     "%(s",
			promptMode: "zsh",
			want:       "(45%%)",
		},
		{
			name:       "bash: borders unaffected",
			format:     "%(s",
			promptMode: "bash",
			want:       "(45%)",
		},
		{
			name:       "zsh: literal percent escaped",
			format:     "%%",
			promptMode: "zsh",
			want:       "%%",
		},
		{
			name:       "bash: literal percent unaffected",
			format:     "%%",
			promptMode: "bash",
			want:       "%",
		},
		// brackets with colors in prompt modes
		{
			name:       "zsh: bracket border with surrounding colors",
			format:     "%{cyan}%[s%{white with reset}",
			promptMode: "zsh",
			want:       "%{\x1b[36m%}[45%%]%{\x1b[37;0m%}",
		},
		{
			name:       "bash: bracket border with surrounding colors",
			format:     "%{cyan}%[s%{white with reset}",
			promptMode: "bash",
			want:       "\001\x1b[36m\002[45%]\001\x1b[37;0m\002",
		},
		{
			name:       "zsh: bracket border between two colors",
			format:     "%{red}%[s%{green}%[w%{white with reset}",
			promptMode: "zsh",
			want:       "%{\x1b[31m%}[45%%]%{\x1b[32m%}[13%%]%{\x1b[37;0m%}",
		},
		// no prompt mode: raw ANSI (default behavior)
		{
			name:       "raw: no wrapping",
			format:     "%{red}hello%{white with reset}",
			promptMode: "",
			want:       "\x1b[31mhello\x1b[37;0m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := displayOptions{sessionPct: 90, weeklyPct: 80, promptMode: tt.promptMode}
			got := formatCustom(tt.format, usage, "pro", opts)
			if got != tt.want {
				t.Errorf("formatCustom(%q) = %q, want %q", tt.format, got, tt.want)
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
		_ = json.NewEncoder(w).Encode(map[string]any{
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
		_, _ = w.Write([]byte("internal error"))
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
		_, _ = w.Write([]byte("not json"))
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

// simulateOAuthCallback parses the auth URL from openBrowser and hits the
// localhost callback server with the given query parameters. If params does
// not contain "state", the real state from the auth URL is used automatically.
func simulateOAuthCallback(t *testing.T, authURL string, params map[string]string) *http.Response {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parsing auth URL: %v", err)
	}
	redirectURI := u.Query().Get("redirect_uri")
	state := u.Query().Get("state")

	cb, err := url.Parse(redirectURI)
	if err != nil {
		t.Fatalf("parsing redirect URI: %v", err)
	}
	q := cb.Query()
	if _, ok := params["state"]; !ok {
		q.Set("state", state)
	}
	for k, v := range params {
		q.Set(k, v)
	}
	cb.RawQuery = q.Encode()

	resp, err := http.Get(cb.String())
	if err != nil {
		t.Fatalf("callback request failed: %v", err)
	}
	return resp
}

func TestLoginOAuth_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "test-access-token",
			"refresh_token": "test-refresh-token",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	oldEndpoint := tokenEndpoint
	tokenEndpoint = srv.URL
	defer func() { tokenEndpoint = oldEndpoint }()

	oldOpenBrowser := openBrowser
	defer func() { openBrowser = oldOpenBrowser }()
	openBrowser = func(authURL string) error {
		go func() {
			resp := simulateOAuthCallback(t, authURL, map[string]string{"code": "test-auth-code"})
			if err := resp.Body.Close(); err != nil {
				t.Errorf("closing response body: %v", err)
			}
		}()
		return nil
	}

	dir := t.TempDir()
	credsPath := filepath.Join(dir, "creds.json")

	creds, err := loginOAuth(credsPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if creds.ClaudeAiOauth.AccessToken != "test-access-token" {
		t.Errorf("access token = %q, want %q", creds.ClaudeAiOauth.AccessToken, "test-access-token")
	}
	if creds.ClaudeAiOauth.RefreshToken != "test-refresh-token" {
		t.Errorf("refresh token = %q, want %q", creds.ClaudeAiOauth.RefreshToken, "test-refresh-token")
	}
	if creds.ClaudeAiOauth.ExpiresAt <= time.Now().UnixMilli() {
		t.Error("expected ExpiresAt to be in the future")
	}

	data, err := os.ReadFile(credsPath)
	if err != nil {
		t.Fatalf("reading saved credentials: %v", err)
	}
	var saved credentials
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("parsing saved credentials: %v", err)
	}
	if saved.ClaudeAiOauth.AccessToken != "test-access-token" {
		t.Errorf("saved access token = %q, want %q", saved.ClaudeAiOauth.AccessToken, "test-access-token")
	}
	if saved.ClaudeAiOauth.RefreshToken != "test-refresh-token" {
		t.Errorf("saved refresh token = %q, want %q", saved.ClaudeAiOauth.RefreshToken, "test-refresh-token")
	}
}

func TestLoginOAuth_TokenExchangeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid_grant"))
	}))
	defer srv.Close()

	oldEndpoint := tokenEndpoint
	tokenEndpoint = srv.URL
	defer func() { tokenEndpoint = oldEndpoint }()

	oldOpenBrowser := openBrowser
	defer func() { openBrowser = oldOpenBrowser }()
	openBrowser = func(authURL string) error {
		go func() {
			resp := simulateOAuthCallback(t, authURL, map[string]string{"code": "test-auth-code"})
			if err := resp.Body.Close(); err != nil {
				t.Errorf("closing response body: %v", err)
			}
		}()
		return nil
	}

	dir := t.TempDir()
	credsPath := filepath.Join(dir, "creds.json")

	_, err := loginOAuth(credsPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "token exchange failed") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "token exchange failed")
	}
}

func TestLoginOAuth_CallbackError(t *testing.T) {
	oldOpenBrowser := openBrowser
	defer func() { openBrowser = oldOpenBrowser }()
	openBrowser = func(authURL string) error {
		go func() {
			resp := simulateOAuthCallback(t, authURL, map[string]string{
				"error":             "access_denied",
				"error_description": "User denied access",
			})
			if err := resp.Body.Close(); err != nil {
				t.Errorf("closing response body: %v", err)
			}
		}()
		return nil
	}

	dir := t.TempDir()
	credsPath := filepath.Join(dir, "creds.json")

	_, err := loginOAuth(credsPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "access_denied") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "access_denied")
	}
}

func TestLoginOAuth_BrowserFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "test-access-token",
			"refresh_token": "test-refresh-token",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	oldEndpoint := tokenEndpoint
	tokenEndpoint = srv.URL
	defer func() { tokenEndpoint = oldEndpoint }()

	oldOpenBrowser := openBrowser
	defer func() { openBrowser = oldOpenBrowser }()
	openBrowser = func(authURL string) error {
		go func() {
			resp := simulateOAuthCallback(t, authURL, map[string]string{"code": "test-auth-code"})
			if err := resp.Body.Close(); err != nil {
				t.Errorf("closing response body: %v", err)
			}
		}()
		return fmt.Errorf("xdg-open: no method available for opening")
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()

	dir := t.TempDir()
	credsPath := filepath.Join(dir, "creds.json")

	creds, err := loginOAuth(credsPath)
	w.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.ClaudeAiOauth.AccessToken != "test-access-token" {
		t.Errorf("access token = %q, want %q", creds.ClaudeAiOauth.AccessToken, "test-access-token")
	}

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	stderr := buf.String()
	if !contains(stderr, "Could not open browser automatically") {
		t.Errorf("stderr = %q, want it to contain browser fallback message", stderr)
	}
	if !contains(stderr, "Please open this URL manually") {
		t.Errorf("stderr = %q, want it to contain manual URL instruction", stderr)
	}
}

func TestLoginOAuth_StateMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "test-access-token",
			"refresh_token": "test-refresh-token",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	oldEndpoint := tokenEndpoint
	tokenEndpoint = srv.URL
	defer func() { tokenEndpoint = oldEndpoint }()

	oldOpenBrowser := openBrowser
	defer func() { openBrowser = oldOpenBrowser }()
	openBrowser = func(authURL string) error {
		go func() {
			// First: bad state — should get 400
			resp := simulateOAuthCallback(t, authURL, map[string]string{
				"code":  "test-auth-code",
				"state": "wrong-state",
			})
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("bad state response = %d, want %d", resp.StatusCode, http.StatusBadRequest)
			}
			if err := resp.Body.Close(); err != nil {
				t.Errorf("closing response body: %v", err)
			}

			// Second: correct state — should succeed
			resp = simulateOAuthCallback(t, authURL, map[string]string{"code": "test-auth-code"})
			if err := resp.Body.Close(); err != nil {
				t.Errorf("closing response body: %v", err)
			}
		}()
		return nil
	}

	dir := t.TempDir()
	credsPath := filepath.Join(dir, "creds.json")

	creds, err := loginOAuth(credsPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if creds.ClaudeAiOauth.AccessToken != "test-access-token" {
		t.Errorf("access token = %q, want %q", creds.ClaudeAiOauth.AccessToken, "test-access-token")
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
