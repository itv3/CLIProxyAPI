package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/officialclient"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

type capturedClaudeOfficialClientRequest struct {
	Header http.Header
	Body   []byte
	Path   string
}

func newClaudeOfficialClientAuth(t *testing.T, baseURL string, enabled bool) *cliproxyauth.Auth {
	t.Helper()
	raw, err := officialclient.EncodeCompatibility(&officialclient.CompatibilityConfig{
		Enabled: enabled,
		Profile: "claude-desktop-2.1.215-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return &cliproxyauth.Auth{
		ID:       "claude-auth-1",
		Provider: "claude",
		Attributes: map[string]string{
			"api_key":                   "secret-key",
			"base_url":                  baseURL,
			"auth_kind":                 "apikey",
			officialclient.AttributeKey: raw,
		},
	}
}

func TestClaudeExecutorOfficialClientProfileNonStream(t *testing.T) {
	requestCh := make(chan capturedClaudeOfficialClientRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestCh <- capturedClaudeOfficialClientRequest{Header: r.Header.Clone(), Body: body, Path: r.URL.Path}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","model":"claude-test","role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{}}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	auth := newClaudeOfficialClientAuth(t, server.URL, true)
	auth.Attributes["header:Authorization"] = "Bearer custom"
	auth.Attributes["header:User-Agent"] = "custom-agent"
	auth.Attributes["header:X-Custom"] = "keep"
	executor := NewClaudeExecutor(&config.Config{})
	response, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model: "claude-test",
		Payload: []byte(`{
			"system":"Keep this rule",
			"messages":[{"role":"user","content":"run it"}],
			"tools":[{"name":"bash","input_schema":{"type":"object"}}],
			"tool_choice":{"type":"auto"}
		}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("claude"),
		Metadata:     map[string]any{cliproxyexecutor.ExecutionSessionMetadataKey: "session-1"},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := gjson.GetBytes(response.Payload, "content.0.name").String(); got != "bash" {
		t.Fatalf("response tool name = %q, want bash; response=%s", got, response.Payload)
	}

	captured := <-requestCh
	profile, _ := officialclient.CurrentProfile("claude")
	if captured.Path != "/v1/messages" {
		t.Fatalf("path = %q", captured.Path)
	}
	if captured.Header.Get("x-api-key") != "secret-key" || captured.Header.Get("Authorization") != "Bearer secret-key" {
		t.Fatalf("authentication headers = %#v", captured.Header)
	}
	if captured.Header.Get("User-Agent") != profile.Claude.FixedHeaders["User-Agent"] || captured.Header.Get("Accept") != "application/json" {
		t.Fatalf("identity headers = %#v", captured.Header)
	}
	if captured.Header.Get("Anthropic-Beta") != strings.Join(profile.Claude.MessagesNonStreamBetas, ",") || captured.Header.Get("X-Stainless-Timeout") != "900" {
		t.Fatalf("messages headers = %#v", captured.Header)
	}
	if captured.Header.Get("X-Client-Request-Id") != "" || captured.Header.Get("X-Stainless-Helper-Method") != "" {
		t.Fatalf("removed headers survived = %#v", captured.Header)
	}
	if captured.Header.Get("X-Custom") != "keep" {
		t.Fatalf("non-protected custom header was lost: %#v", captured.Header)
	}
	if gjson.GetBytes(captured.Body, "stream").Exists() || gjson.GetBytes(captured.Body, "tool_choice").Exists() {
		t.Fatalf("non-stream policy mismatch: %s", captured.Body)
	}
	if got := gjson.GetBytes(captured.Body, "tools.0.name").String(); got != "Bash" {
		t.Fatalf("outgoing tool name = %q, want Bash", got)
	}
	if got := gjson.GetBytes(captured.Body, "system.#").Int(); got != 3 {
		t.Fatalf("system block count = %d, want 3; body=%s", got, captured.Body)
	}
	if got := gjson.GetBytes(captured.Body, "system.2.text").String(); got != "Keep this rule" {
		t.Fatalf("user system text = %q", got)
	}
	billing := gjson.GetBytes(captured.Body, "system.0.text").String()
	if strings.Contains(billing, "cch=00000;") || !strings.Contains(billing, "cc_entrypoint=claude-desktop-3p;") {
		t.Fatalf("billing block was not finalized: %q", billing)
	}
	if userID := gjson.GetBytes(captured.Body, "metadata.user_id").String(); !gjson.Valid(userID) || gjson.Get(userID, "session_id").String() != captured.Header.Get("X-Claude-Code-Session-Id") || len(gjson.Get(userID, "device_id").String()) != 64 {
		t.Fatalf("body/header session mismatch: user_id=%q header=%q", userID, captured.Header.Get("X-Claude-Code-Session-Id"))
	}
}

func TestClaudeExecutorOfficialClientBypassAndConnectivityOverride(t *testing.T) {
	requestCh := make(chan capturedClaudeOfficialClientRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestCh <- capturedClaudeOfficialClientRequest{Header: r.Header.Clone(), Body: body}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","model":"claude-test","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	auth := newClaudeOfficialClientAuth(t, server.URL, true)
	executor := NewClaudeExecutor(&config.Config{})
	req := cliproxyexecutor.Request{Model: "claude-test", Payload: []byte(`{"messages":[{"role":"user","content":"hello"}]}`)}
	officialHeaders := http.Header{"User-Agent": {"claude-cli/2.1.215"}}
	if _, err := executor.Execute(context.Background(), auth, req, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude"), Headers: officialHeaders}); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(context.Background(), auth, req, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("claude"),
		Headers:      officialHeaders,
		Metadata:     map[string]any{cliproxyexecutor.ConnectivityTestMetadataKey: true},
	}); err != nil {
		t.Fatal(err)
	}

	bypassed := <-requestCh
	forced := <-requestCh
	profile, _ := officialclient.CurrentProfile("claude")
	if bypassed.Header.Get("User-Agent") == profile.Claude.FixedHeaders["User-Agent"] || bypassed.Header.Get("Authorization") == "" {
		t.Fatalf("official client did not bypass profile: %#v", bypassed.Header)
	}
	if forced.Header.Get("User-Agent") != profile.Claude.FixedHeaders["User-Agent"] || forced.Header.Get("x-api-key") != "secret-key" || forced.Header.Get("Authorization") != "Bearer secret-key" {
		t.Fatalf("connectivity test did not force profile: %#v", forced.Header)
	}
}

func TestClaudeExecutorOfficialClientProfileHotToggle(t *testing.T) {
	var mu sync.Mutex
	var userAgents []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		userAgents = append(userAgents, r.Header.Get("User-Agent"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","model":"claude-test","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	auth := newClaudeOfficialClientAuth(t, server.URL, true)
	executor := NewClaudeExecutor(&config.Config{})
	req := cliproxyexecutor.Request{Model: "claude-test", Payload: []byte(`{"messages":[{"role":"user","content":"hello"}]}`)}
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude")}
	if _, err := executor.Execute(context.Background(), auth, req, opts); err != nil {
		t.Fatal(err)
	}
	disabled, err := officialclient.EncodeCompatibility(&officialclient.CompatibilityConfig{Profile: "claude-desktop-2.1.215-v1"})
	if err != nil {
		t.Fatal(err)
	}
	auth.Attributes[officialclient.AttributeKey] = disabled
	if _, err := executor.Execute(context.Background(), auth, req, opts); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	got := append([]string(nil), userAgents...)
	mu.Unlock()
	profile, _ := officialclient.CurrentProfile("claude")
	if len(got) != 2 || got[0] != profile.Claude.FixedHeaders["User-Agent"] || got[1] == profile.Claude.FixedHeaders["User-Agent"] {
		t.Fatalf("hot toggle User-Agents = %#v", got)
	}
}

func TestClaudeExecutorOfficialClientProfileStream(t *testing.T) {
	requestCh := make(chan capturedClaudeOfficialClientRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestCh <- capturedClaudeOfficialClientRequest{Header: r.Header.Clone(), Body: body}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\n")
		_, _ = io.WriteString(w, `data: {"type":"message_start","message":{"id":"msg_1","type":"message","model":"claude-test","role":"assistant","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`+"\n\n")
		_, _ = io.WriteString(w, "event: content_block_start\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"Edit","input":{}}}`+"\n\n")
		_, _ = io.WriteString(w, "event: message_delta\n")
		_, _ = io.WriteString(w, `data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":1}}`+"\n\n")
		_, _ = io.WriteString(w, "event: message_stop\n")
		_, _ = io.WriteString(w, `data: {"type":"message_stop"}`+"\n\n")
	}))
	defer server.Close()

	auth := newClaudeOfficialClientAuth(t, server.URL, true)
	executor := NewClaudeExecutor(&config.Config{})
	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-test",
		Payload: []byte(`{"messages":[{"role":"user","content":"edit it"}],"tools":[{"name":"edit","input_schema":{"type":"object"}}]}`),
	}, cliproxyexecutor.Options{
		SourceFormat:   sdktranslator.FromString("claude"),
		ResponseFormat: sdktranslator.FromString("claude"),
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	var output bytes.Buffer
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		output.Write(chunk.Payload)
	}
	if !strings.Contains(output.String(), `"name":"edit"`) || strings.Contains(output.String(), `"name":"Edit"`) {
		t.Fatalf("stream tool name was not restored: %s", output.String())
	}
	captured := <-requestCh
	profile, _ := officialclient.CurrentProfile("claude")
	if !gjson.GetBytes(captured.Body, "stream").Bool() || gjson.GetBytes(captured.Body, "tools.0.name").String() != "Edit" {
		t.Fatalf("outgoing stream body = %s", captured.Body)
	}
	if captured.Header.Get("Accept") != "application/json" || captured.Header.Get("Anthropic-Beta") != strings.Join(profile.Claude.MessagesStreamBetas, ",") {
		t.Fatalf("outgoing stream headers = %#v", captured.Header)
	}
}

func TestClaudeExecutorOfficialClientProfileCountTokens(t *testing.T) {
	requestCh := make(chan capturedClaudeOfficialClientRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestCh <- capturedClaudeOfficialClientRequest{Header: r.Header.Clone(), Body: body, Path: r.URL.Path}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"input_tokens":42}`))
	}))
	defer server.Close()

	auth := newClaudeOfficialClientAuth(t, server.URL, true)
	executor := NewClaudeExecutor(&config.Config{})
	_, err := executor.CountTokens(context.Background(), auth, cliproxyexecutor.Request{
		Model: "claude-test",
		Payload: []byte(`{
			"messages":[{"role":"user","content":"count it"}],
			"temperature":1,"top_p":0.9,"top_k":10,"stream":true,"stop_sequences":["x"],
			"tools":[{"name":"write","input_schema":{"type":"object"}}]
		}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude")})
	if err != nil {
		t.Fatalf("CountTokens() error = %v", err)
	}
	captured := <-requestCh
	profile, _ := officialclient.CurrentProfile("claude")
	if captured.Path != "/v1/messages/count_tokens" || captured.Header.Get("X-Stainless-Timeout") != "" {
		t.Fatalf("count_tokens path/headers = %q %#v", captured.Path, captured.Header)
	}
	if captured.Header.Get("Anthropic-Beta") != strings.Join(profile.Claude.CountTokensBetas, ",") {
		t.Fatalf("count_tokens beta = %q", captured.Header.Get("Anthropic-Beta"))
	}
	for _, path := range []string{"temperature", "top_p", "top_k", "stream", "stop_sequences", "metadata", "output_config", "thinking"} {
		if gjson.GetBytes(captured.Body, path).Exists() {
			t.Fatalf("count_tokens body retained/injected %q: %s", path, captured.Body)
		}
	}
	if got := gjson.GetBytes(captured.Body, "tools.0.name").String(); got != "Write" {
		t.Fatalf("count_tokens tool name = %q, want Write", got)
	}
}

func TestClaudeExecutorOfficialClientInvalidRuntimeConfigIsRequestScoped(t *testing.T) {
	auth := newClaudeOfficialClientAuth(t, "http://127.0.0.1:1", true)
	auth.Attributes[officialclient.AttributeKey] = `{"enabled":true,"profile":"missing","tls-profile":""}`
	_, err := NewClaudeExecutor(&config.Config{}).Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-test",
		Payload: []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("claude")})
	if err == nil {
		t.Fatal("invalid runtime config unexpectedly succeeded")
	}
	requestScoped, ok := err.(interface{ IsRequestScoped() bool })
	if !ok || !requestScoped.IsRequestScoped() {
		t.Fatalf("error is not request scoped: %T %v", err, err)
	}
}
