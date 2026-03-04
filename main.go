package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fatih/color"
)

var colorMap = map[string]color.Attribute{
	"black":     color.FgBlack,
	"red":       color.FgRed,
	"green":     color.FgGreen,
	"yellow":    color.FgYellow,
	"blue":      color.FgBlue,
	"magenta":   color.FgMagenta,
	"cyan":      color.FgCyan,
	"white":     color.FgWhite,
	"hiblack":   color.FgHiBlack,
	"hired":     color.FgHiRed,
	"higreen":   color.FgHiGreen,
	"hiyellow":  color.FgHiYellow,
	"hiblue":    color.FgHiBlue,
	"himagenta": color.FgHiMagenta,
	"hicyan":    color.FgHiCyan,
	"hiwhite":   color.FgHiWhite,
}

var bgColorMap = map[string]color.Attribute{
	"black":     color.BgBlack,
	"red":       color.BgRed,
	"green":     color.BgGreen,
	"yellow":    color.BgYellow,
	"blue":      color.BgBlue,
	"magenta":   color.BgMagenta,
	"cyan":      color.BgCyan,
	"white":     color.BgWhite,
	"hiblack":   color.BgHiBlack,
	"hired":     color.BgHiRed,
	"higreen":   color.BgHiGreen,
	"hiyellow":  color.BgHiYellow,
	"hiblue":    color.BgHiBlue,
	"himagenta": color.BgHiMagenta,
	"hicyan":    color.BgHiCyan,
	"hiwhite":   color.BgHiWhite,
}

var attrMap = map[string]color.Attribute{
	"reset":        color.Reset,
	"bold":         color.Bold,
	"faint":        color.Faint,
	"italic":       color.Italic,
	"underline":    color.Underline,
	"blinkslow":    color.BlinkSlow,
	"blinkrapid":   color.BlinkRapid,
	"reversevideo": color.ReverseVideo,
	"concealed":    color.Concealed,
	"crossedout":   color.CrossedOut,
}

// colorSpec parses a colt-style color specification and returns the ANSI escape sequence.
// Grammar: "fg [on bg] [with attr ...]"
func colorSpec(spec string) string {
	words := strings.Fields(spec)
	if len(words) == 0 {
		return ""
	}

	var codes []string

	fg := words[0]
	if a, ok := colorMap[fg]; ok {
		codes = append(codes, fmt.Sprintf("%d", a))
	}

	i := 1
	for i < len(words) {
		switch words[i] {
		case "on":
			i++
			if i < len(words) {
				if a, ok := bgColorMap[words[i]]; ok {
					codes = append(codes, fmt.Sprintf("%d", a))
				}
				i++
			}
		case "with":
			i++
			for i < len(words) {
				if a, ok := attrMap[words[i]]; ok {
					codes = append(codes, fmt.Sprintf("%d", a))
				}
				i++
			}
		default:
			i++
		}
	}

	if len(codes) == 0 {
		return ""
	}

	return fmt.Sprintf("\x1b[%sm", strings.Join(codes, ";"))
}

const oauthClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

var tokenEndpoint = "https://platform.claude.com/v1/oauth/token"

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
	defer func() { _ = resp.Body.Close() }()

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
	promptMode   string
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

// mirrorRune returns the mirrored counterpart of paired Unicode characters.
// Parentheses, brackets, angle brackets, arrows, triangles, and other
// directional or paired glyphs are mapped to their mirror image.
func mirrorRune(r rune) rune {
	switch r {
	case '(':
		return ')'
	case ')':
		return '('
	case '[':
		return ']'
	case ']':
		return '['
	case '<':
		return '>'
	case '>':
		return '<'
	// unicode arrows
	case '←':
		return '→'
	case '→':
		return '←'
	case '⇐':
		return '⇒'
	case '⇒':
		return '⇐'
	case '⟵':
		return '⟶'
	case '⟶':
		return '⟵'
	case '⟸':
		return '⟹'
	case '⟹':
		return '⟸'
	// unicode triangles
	case '◀':
		return '▶'
	case '▶':
		return '◀'
	case '◁':
		return '▷'
	case '▷':
		return '◁'
	case '◂':
		return '▸'
	case '▸':
		return '◂'
	case '◃':
		return '▹'
	case '▹':
		return '◃'
	case '▲':
		return '▼'
	case '▼':
		return '▲'
	case '△':
		return '▽'
	case '▽':
		return '△'
	// unicode angle brackets and chevrons
	case '⟨':
		return '⟩'
	case '⟩':
		return '⟨'
	case '❨':
		return '❩'
	case '❩':
		return '❨'
	case '❪':
		return '❫'
	case '❫':
		return '❪'
	case '❬':
		return '❭'
	case '❭':
		return '❬'
	case '❮':
		return '❯'
	case '❯':
		return '❮'
	case '❰':
		return '❱'
	case '❱':
		return '❰'
	case '«':
		return '»'
	case '»':
		return '«'
	case '‹':
		return '›'
	case '›':
		return '‹'
	// mathematical/misc paired
	case '⌈':
		return '⌉'
	case '⌉':
		return '⌈'
	case '⌊':
		return '⌋'
	case '⌋':
		return '⌊'
	default:
		return r
	}
}

// reverseModifier builds the closing delimiter from a modifier string.
// Each rune is replaced with its mirror counterpart, then the entire
// string is reversed so the closing side is the visual mirror image.
func reverseModifier(mod string) string {
	runes := []rune(mod)
	for i, r := range runes {
		runes[i] = mirrorRune(r)
	}
	for l, r := 0, len(runes)-1; l < r; l, r = l+1, r-1 {
		runes[l], runes[r] = runes[r], runes[l]
	}
	return string(runes)
}

// findVerb returns the byte index of the first format verb character
// at or after start, or -1 if none is found.
func findVerb(format string, start int) int {
	for j := start; j < len(format); j++ {
		switch format[j] {
		case 'p', 's', 'S', 'w', 'W', 't', 'T', '%':
			return j
		}
	}
	return -1
}

func formatCustom(format string, usage usageResponse, plan string, opts displayOptions) string {
	if plan == "" {
		plan = "Unknown"
	} else {
		plan = strings.ToUpper(plan[:1]) + plan[1:]
	}

	// zshText escapes % as %% when in zsh prompt mode, so zsh doesn't
	// interpret content characters as prompt escapes.
	zshText := func(s string) string {
		if opts.promptMode == "zsh" {
			return strings.ReplaceAll(s, "%", "%%")
		}
		return s
	}

	var out strings.Builder
	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 >= len(format) {
			out.WriteByte(format[i])
			continue
		}
		i++

		// %{color spec} — colt-style color code
		if format[i] == '{' {
			end := strings.IndexByte(format[i:], '}')
			if end != -1 {
				spec := format[i+1 : i+end]
				code := colorSpec(spec)
				if code != "" {
					switch opts.promptMode {
					case "zsh":
						code = "%{" + code + "%}"
					case "bash":
						code = "\001" + code + "\002"
					}
				}
				out.WriteString(code)
				i += end
				continue
			}
			// no closing brace, pass through literally
			out.WriteString(zshText("%{"))
			continue
		}

		// scan for verb, collecting modifier characters
		verbIdx := findVerb(format, i)
		if verbIdx < 0 {
			// no verb found, output everything literally
			out.WriteString(zshText("%" + format[i:]))
			i = len(format) - 1
			continue
		}

		modifier := format[i:verbIdx]
		i = verbIdx

		var open, close string
		if len(modifier) > 0 {
			open = modifier
			close = reverseModifier(modifier)
		}

		var content string
		switch format[i] {
		case 'p':
			content = plan
		case 's':
			if usage.FiveHour != nil {
				content = formatPct(usage.FiveHour.Utilization, opts.remaining)
			}
		case 'S':
			if usage.FiveHour != nil {
				content = formatReset(usage.FiveHour.ResetsAt, "3:04pm")
			}
		case 'w':
			if usage.SevenDay != nil {
				content = formatPct(usage.SevenDay.Utilization, opts.remaining)
			}
		case 'W':
			if usage.SevenDay != nil {
				content = formatReset(usage.SevenDay.ResetsAt, "Mon 3:04pm")
			}
		case 't':
			if usage.FiveHour != nil && usage.FiveHour.Utilization >= opts.sessionPct {
				if s := formatReset(usage.FiveHour.ResetsAt, "3:04pm"); s != "" {
					content = "resets " + s
				}
			}
		case 'T':
			if usage.SevenDay != nil && usage.SevenDay.Utilization >= opts.weeklyPct {
				if s := formatReset(usage.SevenDay.ResetsAt, "Mon 3:04pm"); s != "" {
					content = "resets " + s
				}
			}
		case '%':
			out.WriteString(zshText("%"))
			continue
		default:
			out.WriteString(zshText("%" + modifier + string(format[i])))
			continue
		}

		if content != "" {
			out.WriteString(zshText(open + content + close))
		}
	}
	return out.String()
}

func stripCR(s string) string {
	return strings.ReplaceAll(s, "\r", "")
}

var openBrowser = func(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Run()
	case "linux":
		return exec.Command("xdg-open", url).Run()
	default:
		return fmt.Errorf("unsupported platform")
	}
}

func loginOAuth(credsPath string) (*credentials, error) {
	// Generate PKCE code verifier (32 random bytes, base64url-encoded)
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return nil, fmt.Errorf("generating code verifier: %w", err)
	}
	codeVerifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

	// Code challenge = base64url(sha256(code_verifier))
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	// Random state parameter
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, fmt.Errorf("generating state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)

	// Start localhost server on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("starting local server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	// Build authorization URL
	authURL := (&url.URL{
		Scheme: "https",
		Host:   "claude.ai",
		Path:   "/oauth/authorize",
		RawQuery: url.Values{
			"code":                  {"true"},
			"client_id":             {oauthClientID},
			"response_type":         {"code"},
			"redirect_uri":          {redirectURI},
			"scope":                 {"user:profile user:inference user:sessions:claude_code user:mcp_servers"},
			"code_challenge":        {codeChallenge},
			"code_challenge_method": {"S256"},
			"state":                 {state},
		}.Encode(),
	}).String()

	// Channel to receive the authorization code
	type callbackResult struct {
		code string
		err  error
	}
	resultCh := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			desc := r.URL.Query().Get("error_description")
			resultCh <- callbackResult{err: fmt.Errorf("OAuth error: %s: %s", errParam, desc)}
			_, _ = fmt.Fprintf(w, "Authentication failed: %s", errParam)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			resultCh <- callbackResult{err: fmt.Errorf("no authorization code received")}
			http.Error(w, "No code received", http.StatusBadRequest)
			return
		}
		resultCh <- callbackResult{code: code}
		_, _ = fmt.Fprint(w, "Authentication successful! You can close this tab.")
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()

	// Open browser
	fmt.Fprintln(os.Stderr, "Opening browser to authenticate with Claude...")
	if err := openBrowser(authURL); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser automatically.\nPlease open this URL manually:\n%s\n", authURL)
	}

	// Wait for callback
	result := <-resultCh
	_ = srv.Shutdown(context.Background())
	if result.err != nil {
		return nil, result.err
	}

	// Exchange authorization code for tokens
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"code":          result.code,
		"code_verifier": codeVerifier,
		"redirect_uri":  redirectURI,
		"client_id":     oauthClientID,
	})

	resp, err := http.Post(tokenEndpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, b)
	}

	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	creds := &credentials{
		ClaudeAiOauth: oauthCredentials{
			AccessToken:  tok.AccessToken,
			RefreshToken: tok.RefreshToken,
			ExpiresAt:    time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).UnixMilli(),
		},
	}

	// Persist credentials
	if err := os.MkdirAll(filepath.Dir(credsPath), 0700); err != nil {
		return nil, fmt.Errorf("creating credentials directory: %w", err)
	}
	data, err := json.Marshal(creds)
	if err != nil {
		return nil, fmt.Errorf("marshaling credentials: %w", err)
	}
	if err := os.WriteFile(credsPath, data, 0600); err != nil {
		return nil, fmt.Errorf("writing credentials: %w", err)
	}

	return creds, nil
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] [FORMAT ...]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Trailing arguments are joined and used as a format string.\n")
		fmt.Fprintf(os.Stderr, "Format verbs: %%p plan, %%s session%%, %%S session reset, %%t session reset (threshold),\n")
		fmt.Fprintf(os.Stderr, "              %%w weekly%%, %%W weekly reset, %%T weekly reset (threshold), %%%% literal %%\n")
		fmt.Fprintf(os.Stderr, "Color:        %%{fg [on bg] [with attr ...]}  (colt-style color spec)\n")
		fmt.Fprintf(os.Stderr, "Border:       %%<chars><verb> to enclose output; right side is mirrored & reversed\n")
		fmt.Fprintf(os.Stderr, "              Paired chars: ( ) [ ] < > « » ‹ › ← → ⟨ ⟩ ◀ ▶ and more\n")
		fmt.Fprintf(os.Stderr, "Prompt:       -p zsh wraps ANSI in %%{...%%}, -p bash wraps in \\001...\\002\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	sessionPct := flag.Float64("s", 90, "show reset time when session usage exceeds this %")
	weeklyPct := flag.Float64("w", 80, "show weekly usage when it exceeds this %")
	alwaysWeekly := flag.Bool("W", false, "always show weekly usage")
	remaining := flag.Bool("r", false, "show remaining capacity instead of usage")
	jsonOutput := flag.Bool("j", false, "output usage as JSON")
	promptMode := flag.String("p", "", "wrap ANSI codes for prompt use: zsh or bash")
	flag.Parse()

	if *promptMode != "" && *promptMode != "zsh" && *promptMode != "bash" {
		fmt.Fprintf(os.Stderr, "error: -p must be 'zsh' or 'bash', got %q\n", *promptMode)
		os.Exit(1)
	}

	var creds credentials
	var apiKey string

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	credsPath := filepath.Join(home, ".claude", ".credentials.json")
	data, err := os.ReadFile(credsPath)
	if err == nil {
		if err := json.Unmarshal(data, &creds); err != nil {
			fmt.Fprintf(os.Stderr, "error parsing credentials: %v\n", err)
			os.Exit(1)
		}
	}

	if creds.ClaudeAiOauth.AccessToken == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			loginCreds, err := loginOAuth(credsPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "OAuth login failed: %v\n", err)
				os.Exit(1)
			}
			creds = *loginCreds
		}
	} else if time.Now().UnixMilli() >= creds.ClaudeAiOauth.ExpiresAt {
		if err := refreshToken(&creds, credsPath); err != nil {
			fmt.Fprintf(os.Stderr, "token refresh failed: %v\nRe-authenticating...\n", err)
			loginCreds, err := loginOAuth(credsPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "OAuth login failed: %v\n", err)
				os.Exit(1)
			}
			creds = *loginCreds
		}
	}

	req, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+creds.ClaudeAiOauth.AccessToken)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if apiKey == "" {
			fmt.Fprintf(os.Stderr, "Authentication expired or revoked. Re-authenticating...\n")
			loginCreds, err := loginOAuth(credsPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "OAuth login failed: %v\n", err)
				os.Exit(1)
			}
			creds = *loginCreds

			req2, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			req2.Header.Set("Authorization", "Bearer "+creds.ClaudeAiOauth.AccessToken)
			req2.Header.Set("Content-Type", "application/json")
			req2.Header.Set("anthropic-beta", "oauth-2025-04-20")

			resp2, err := http.DefaultClient.Do(req2)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			defer func() { _ = resp2.Body.Close() }()
			respBody, _ = io.ReadAll(resp2.Body)
			if resp2.StatusCode != http.StatusOK {
				fmt.Fprintf(os.Stderr, "API error %d: %s\n", resp2.StatusCode, respBody)
				os.Exit(1)
			}
		} else {
			fmt.Fprintf(os.Stderr, "API error %d: %s\n", resp.StatusCode, respBody)
			os.Exit(1)
		}
	} else if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "API error %d: %s\n", resp.StatusCode, respBody)
		os.Exit(1)
	}

	var usage usageResponse
	if err := json.Unmarshal(respBody, &usage); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing usage: %v\n", err)
		os.Exit(1)
	}

	opts := displayOptions{
		sessionPct:   *sessionPct,
		weeklyPct:    *weeklyPct,
		alwaysWeekly: *alwaysWeekly,
		remaining:    *remaining,
		promptMode:   *promptMode,
	}

	if args := flag.Args(); len(args) > 0 {
		formatStr := strings.Join(args, " ")
		fmt.Print(stripCR(formatCustom(formatStr, usage, creds.ClaudeAiOauth.SubscriptionType, opts)))
		return
	}

	if *jsonOutput {
		s, err := formatJSON(usage)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error marshaling JSON: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(stripCR(s))
		return
	}

	fmt.Print(stripCR(formatDisplay(usage, creds.ClaudeAiOauth.SubscriptionType, opts)))
}
