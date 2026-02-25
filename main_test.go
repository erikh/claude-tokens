package main

import (
	"encoding/json"
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
