package executor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/officialclient"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

type capturedCodexOfficialClientRequest struct {
	Header http.Header
	Body   []byte
	Path   string
}

func newCodexOfficialClientAuth(t *testing.T, baseURL string, enabled bool) *cliproxyauth.Auth {
	t.Helper()
	raw, err := officialclient.EncodeCompatibility(&officialclient.CompatibilityConfig{
		Enabled: enabled,
		Profile: "codex-desktop-0.145.0-alpha.18-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return &cliproxyauth.Auth{
		ID:       "codex-auth-1",
		Provider: "codex",
		Attributes: map[string]string{
			"api_key":                   "secret-key",
			"base_url":                  baseURL,
			"auth_kind":                 "apikey",
			officialclient.AttributeKey: raw,
		},
	}
}

func newCodexOfficialClientTestServer(t *testing.T, requestCh chan<- capturedCodexOfficialClientRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestCh <- capturedCodexOfficialClientRequest{Header: r.Header.Clone(), Body: body, Path: r.URL.Path}
		if r.URL.Path == "/v1/responses/compact" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"resp_compact","object":"response.compaction","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-5.6-sol\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
}

func TestCodexExecutorOfficialClientProfileResponsesAndStream(t *testing.T) {
	requestCh := make(chan capturedCodexOfficialClientRequest, 2)
	server := newCodexOfficialClientTestServer(t, requestCh)
	defer server.Close()

	auth := newCodexOfficialClientAuth(t, server.URL, true)
	auth.Attributes["header:Authorization"] = "Bearer custom"
	auth.Attributes["header:User-Agent"] = "custom-agent"
	auth.Attributes["header:Originator"] = "custom-originator"
	auth.Attributes["header:Content-Type"] = "text/plain"
	auth.Attributes["header:Session_id"] = "custom-session"
	auth.Attributes["header:OpenAI-Beta"] = "responses_websockets=2026-02-06"
	auth.Attributes["header:X-Custom"] = "keep"
	executor := NewCodexExecutor(&config.Config{
		SDKConfig: config.SDKConfig{DisableImageGeneration: config.DisableImageGenerationAll},
		Codex:     config.CodexConfig{IdentityConfuse: true},
	})
	payload := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[{"role":"user","content":"hello"}],
		"prompt_cache_key":"client-cache",
		"stream_options":{"reasoning_summary_delivery":"sequential_cutoff"},
		"parallel_tool_calls":false,
		"client_metadata":{"account_state":"remove"}
	}`)
	opts := cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai-response"),
		Metadata:     map[string]any{cliproxyexecutor.ExecutionSessionMetadataKey: "logical-session"},
	}
	if _, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{Model: "gpt-5.6-sol", Payload: payload}, opts); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	streamResult, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{Model: "gpt-5.6-sol", Payload: payload}, opts)
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	for chunk := range streamResult.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error = %v", chunk.Err)
		}
	}

	first := <-requestCh
	second := <-requestCh
	assertCodexExecutorOfficialClientResponsesRequest(t, first)
	assertCodexExecutorOfficialClientResponsesRequest(t, second)
	if gjson.GetBytes(first.Body, "prompt_cache_key").String() != gjson.GetBytes(second.Body, "prompt_cache_key").String() {
		t.Fatal("logical session identity changed between Execute and ExecuteStream")
	}
	if gjson.GetBytes(first.Body, "client_metadata.turn_id").String() == gjson.GetBytes(second.Body, "client_metadata.turn_id").String() {
		t.Fatal("turn identity was reused between Execute and ExecuteStream")
	}
}

func TestCodexExecutorOfficialClientProfileCompact(t *testing.T) {
	requestCh := make(chan capturedCodexOfficialClientRequest, 1)
	server := newCodexOfficialClientTestServer(t, requestCh)
	defer server.Close()

	auth := newCodexOfficialClientAuth(t, server.URL, true)
	executor := NewCodexExecutor(&config.Config{
		SDKConfig: config.SDKConfig{DisableImageGeneration: config.DisableImageGenerationAll},
		Codex:     config.CodexConfig{IdentityConfuse: true},
	})
	payload := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[{"role":"system","content":"history"}],
		"previous_response_id":"resp-1",
		"parallel_tool_calls":false,
		"reasoning":{"effort":"high"},
		"text":{"verbosity":"medium"},
		"tools":[],
		"stream":true,
		"store":false,
		"prompt_cache_key":"remove",
		"client_metadata":{"remove":true},
		"service_tier":"priority"
	}`)
	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{Model: "gpt-5.6-sol", Payload: payload}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai-response"),
		Alt:          "responses/compact",
	})
	if err != nil {
		t.Fatalf("compact Execute() error = %v", err)
	}
	captured := <-requestCh
	if captured.Path != "/v1/responses/compact" || captured.Header.Get("Accept") != "application/json" {
		t.Fatalf("compact path/headers = %q/%#v", captured.Path, captured.Header)
	}
	for _, name := range []string{"stream", "store", "prompt_cache_key", "client_metadata", "service_tier", "include"} {
		if gjson.GetBytes(captured.Body, name).Exists() {
			t.Fatalf("compact field %q survived: %s", name, captured.Body)
		}
	}
	if gjson.GetBytes(captured.Body, "instructions").Exists() {
		t.Fatalf("compact injected instructions: %s", captured.Body)
	}
	if gjson.GetBytes(captured.Body, "input.0.role").String() != "system" || gjson.GetBytes(captured.Body, "input.0.type").Exists() {
		t.Fatalf("compact input was normalized as ordinary Responses: %s", captured.Body)
	}
}

func TestCodexExecutorOfficialClientBypassConnectivityAndHotToggle(t *testing.T) {
	requestCh := make(chan capturedCodexOfficialClientRequest, 3)
	server := newCodexOfficialClientTestServer(t, requestCh)
	defer server.Close()
	auth := newCodexOfficialClientAuth(t, server.URL, true)
	auth.Attributes["header:User-Agent"] = "account-custom-agent"
	executor := NewCodexExecutor(&config.Config{SDKConfig: config.SDKConfig{DisableImageGeneration: config.DisableImageGenerationAll}})
	req := cliproxyexecutor.Request{Model: "gpt-5.6-sol", Payload: []byte(`{"input":[{"role":"user","content":"hello"}]}`)}
	officialHeaders := http.Header{"Originator": {"Codex Desktop"}}
	if _, err := executor.Execute(context.Background(), auth, req, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai-response"), Headers: officialHeaders}); err != nil {
		t.Fatalf("official bypass error = %v", err)
	}
	if _, err := executor.Execute(context.Background(), auth, req, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai-response"),
		Headers:      officialHeaders,
		Metadata:     map[string]any{cliproxyexecutor.ConnectivityTestMetadataKey: true},
	}); err != nil {
		t.Fatalf("connectivity override error = %v", err)
	}
	disabled, err := officialclient.EncodeCompatibility(&officialclient.CompatibilityConfig{Profile: "codex-desktop-0.145.0-alpha.18-v1"})
	if err != nil {
		t.Fatal(err)
	}
	auth.Attributes[officialclient.AttributeKey] = disabled
	if _, err := executor.Execute(context.Background(), auth, req, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai-response")}); err != nil {
		t.Fatalf("hot toggle disabled error = %v", err)
	}

	bypassed := <-requestCh
	forced := <-requestCh
	disabledRequest := <-requestCh
	profile, _ := officialclient.CurrentProfile("codex")
	profileUserAgent := profile.Codex.ResponsesHeaders["User-Agent"]
	if bypassed.Header.Get("User-Agent") == profileUserAgent || bypassed.Header.Get("User-Agent") != "account-custom-agent" {
		t.Fatalf("official request did not bypass profile: %#v", bypassed.Header)
	}
	if forced.Header.Get("User-Agent") != profileUserAgent {
		t.Fatalf("connectivity test did not force profile: %#v", forced.Header)
	}
	if disabledRequest.Header.Get("User-Agent") == profileUserAgent || disabledRequest.Header.Get("User-Agent") != "account-custom-agent" {
		t.Fatalf("hot toggle did not disable profile: %#v", disabledRequest.Header)
	}
}

func TestCodexAutoExecutorOfficialClientProfileRouting(t *testing.T) {
	auth := newCodexOfficialClientAuth(t, "http://127.0.0.1", true)
	auth.Attributes["websockets"] = "true"
	ctx := cliproxyexecutor.WithDownstreamWebsocket(context.Background())

	useWebsocket, err := codexAutoUseWebsocket(ctx, auth, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("profile route error = %v", err)
	}
	if useWebsocket {
		t.Fatal("non-official Profile request selected upstream WebSocket")
	}
	useWebsocket, err = codexAutoUseWebsocket(ctx, auth, cliproxyexecutor.Options{Headers: http.Header{"Originator": {"Codex Desktop"}}})
	if err != nil {
		t.Fatalf("official route error = %v", err)
	}
	if !useWebsocket {
		t.Fatal("official Codex request did not preserve upstream WebSocket selection")
	}
	useWebsocket, err = codexAutoUseWebsocket(ctx, auth, cliproxyexecutor.Options{
		Headers:  http.Header{"Originator": {"Codex Desktop"}},
		Metadata: map[string]any{cliproxyexecutor.ConnectivityTestMetadataKey: true},
	})
	if err != nil {
		t.Fatalf("connectivity route error = %v", err)
	}
	if useWebsocket {
		t.Fatal("connectivity test selected upstream WebSocket")
	}

	auth.Attributes[officialclient.AttributeKey] = `{`
	_, err = codexAutoUseWebsocket(ctx, auth, cliproxyexecutor.Options{})
	if err == nil {
		t.Fatal("malformed compatibility config did not fail closed")
	}
	var requestScoped interface{ IsRequestScoped() bool }
	if !strings.Contains(err.Error(), officialclient.ErrInvalidAttributes.Error()) || !errors.As(err, &requestScoped) || !requestScoped.IsRequestScoped() {
		t.Fatalf("route error is not request-scoped: %v", err)
	}
}

func assertCodexExecutorOfficialClientResponsesRequest(t *testing.T, captured capturedCodexOfficialClientRequest) {
	t.Helper()
	profile, _ := officialclient.CurrentProfile("codex")
	if captured.Path != "/v1/responses" {
		t.Fatalf("path = %q", captured.Path)
	}
	if captured.Header.Get("Authorization") != "Bearer secret-key" || captured.Header.Get("X-Api-Key") != "" {
		t.Fatalf("authentication headers = %#v", captured.Header)
	}
	if captured.Header.Get("User-Agent") != profile.Codex.ResponsesHeaders["User-Agent"] || captured.Header.Get("Originator") != "Codex Desktop" || captured.Header.Get("Accept") != "text/event-stream" || captured.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("fixed headers = %#v", captured.Header)
	}
	if captured.Header.Get("OpenAI-Beta") != "" || captured.Header.Get("Session_id") != "" {
		t.Fatalf("removed headers survived = %#v", captured.Header)
	}
	if captured.Header.Get("X-Custom") != "keep" {
		t.Fatalf("non-protected custom header was lost: %#v", captured.Header)
	}
	if gjson.GetBytes(captured.Body, "instructions").Exists() {
		t.Fatalf("Profile injected instructions: %s", captured.Body)
	}
	if !gjson.GetBytes(captured.Body, "stream").Bool() || gjson.GetBytes(captured.Body, "store").Bool() || gjson.GetBytes(captured.Body, "stream_options.reasoning_summary_delivery").String() != "sequential_cutoff" {
		t.Fatalf("Responses body policy mismatch: %s", captured.Body)
	}
	if gjson.GetBytes(captured.Body, "client_metadata.account_state").Exists() {
		t.Fatalf("private client metadata survived: %s", captured.Body)
	}
	sessionID := gjson.GetBytes(captured.Body, "client_metadata.session_id").String()
	if sessionID == "" || gjson.GetBytes(captured.Body, "prompt_cache_key").String() != sessionID || captured.Header.Get("Session-Id") != sessionID || captured.Header.Get("Thread-Id") != sessionID || captured.Header.Get("X-Client-Request-Id") != sessionID {
		t.Fatalf("body/header session relation mismatch: body=%s headers=%#v", captured.Body, captured.Header)
	}
	if captured.Header.Get("X-Codex-Turn-Metadata") != gjson.GetBytes(captured.Body, "client_metadata.x-codex-turn-metadata").String() {
		t.Fatalf("turn metadata relation mismatch: body=%s headers=%#v", captured.Body, captured.Header)
	}
}

func TestCodexOfficialClientEndpointURL(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{name: "root", base: "https://upstream.example", want: "https://upstream.example/v1/responses"},
		{name: "v1", base: "https://upstream.example/v1", want: "https://upstream.example/v1/responses"},
		{name: "prefix", base: "https://upstream.example/openai", want: "https://upstream.example/openai/v1/responses"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := codexUpstreamEndpointURL(test.base, "/v1/responses", true); got != test.want {
				t.Fatalf("codexUpstreamEndpointURL() = %q, want %q", got, test.want)
			}
		})
	}
	if got := codexUpstreamEndpointURL("https://upstream.example", "/v1/responses", false); got != "https://upstream.example/responses" {
		t.Fatalf("disabled compatibility URL = %q", got)
	}
}
