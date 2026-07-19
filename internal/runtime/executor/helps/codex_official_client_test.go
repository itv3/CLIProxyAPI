package helps

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/officialclient"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
)

func TestApplyCodexOfficialClientResponsesProfile(t *testing.T) {
	profile, ok := officialclient.LookupProfile("codex-desktop-0.145.0-alpha.18-v1")
	if !ok {
		t.Fatal("Codex profile not found")
	}
	payload := []byte(`{
		"model":"gpt-5.6-sol",
		"store":true,
		"stream":false,
		"instructions":"keep instructions",
		"include":["file_search_call.results"],
		"reasoning":{"effort":"high","summary":"detailed"},
		"text":{"format":{"type":"text"}},
		"parallel_tool_calls":false,
		"stream_options":{"reasoning_summary_delivery":"sequential_cutoff"},
		"tool_choice":"auto",
		"tools":[{"type":"function","name":"shell"}],
		"prompt_cache_key":"must-not-drive-profile-session",
		"client_metadata":{"account_state":"secret","local_paths":["/tmp/secret"]},
		"input":[
			{"role":"system","content":"system guidance"},
			{"role":"user","content":"hello"}
		]
	}`)
	metadata := map[string]any{cliproxyexecutor.ExecutionSessionMetadataKey: "logical-session"}

	body, state, err := ApplyCodexOfficialClientResponsesProfile(profile, "auth-1", payload, nil, metadata)
	if err != nil {
		t.Fatalf("ApplyCodexOfficialClientResponsesProfile() error = %v", err)
	}
	if gjson.GetBytes(body, "store").Bool() || !gjson.GetBytes(body, "stream").Bool() {
		t.Fatalf("stream/store policy mismatch: %s", body)
	}
	if got := gjson.GetBytes(body, "reasoning.context").String(); got != "all_turns" {
		t.Fatalf("reasoning.context = %q", got)
	}
	if got := gjson.GetBytes(body, "text.verbosity").String(); got != "low" {
		t.Fatalf("text.verbosity = %q", got)
	}
	if got := gjson.GetBytes(body, "instructions").String(); got != "keep instructions" {
		t.Fatalf("instructions = %q", got)
	}
	if got := gjson.GetBytes(body, "stream_options.reasoning_summary_delivery").String(); got != "sequential_cutoff" {
		t.Fatalf("stream_options was not preserved: %s", body)
	}
	if got := gjson.GetBytes(body, "parallel_tool_calls"); !got.Exists() || got.Bool() {
		t.Fatalf("parallel_tool_calls was not preserved: %s", body)
	}
	if got := gjson.GetBytes(body, "input.0.type").String(); got != "message" {
		t.Fatalf("input.0.type = %q", got)
	}
	if got := gjson.GetBytes(body, "input.0.role").String(); got != "developer" {
		t.Fatalf("input.0.role = %q", got)
	}
	if got := gjson.GetBytes(body, "input.1.content.0.type").String(); got != "input_text" {
		t.Fatalf("bare user content was not normalized: %s", body)
	}
	include := gjson.GetBytes(body, "include").Array()
	if len(include) != 2 || include[0].String() != "file_search_call.results" || include[1].String() != "reasoning.encrypted_content" {
		t.Fatalf("include = %s", gjson.GetBytes(body, "include").Raw)
	}
	clientMetadata := gjson.GetBytes(body, "client_metadata")
	if len(clientMetadata.Map()) != 6 || clientMetadata.Get("account_state").Exists() || clientMetadata.Get("local_paths").Exists() {
		t.Fatalf("client_metadata = %s", clientMetadata.Raw)
	}
	if got := gjson.GetBytes(body, "prompt_cache_key").String(); got != state.SessionID {
		t.Fatalf("prompt_cache_key = %q, want %q", got, state.SessionID)
	}
	assertCodexOfficialClientIdentityRelations(t, body, state)
}

func TestCodexOfficialClientIdentityLifecycleDoesNotUsePromptCacheKey(t *testing.T) {
	profile, _ := officialclient.LookupProfile("codex-desktop-0.145.0-alpha.18-v1")
	metadata := map[string]any{cliproxyexecutor.ExecutionSessionMetadataKey: "session-a"}
	first, firstState, err := ApplyCodexOfficialClientResponsesProfile(profile, "auth-1", []byte(`{"prompt_cache_key":"api-key-shaped-a","input":[{"role":"user","content":"hello"}]}`), nil, metadata)
	if err != nil {
		t.Fatalf("first request error = %v", err)
	}
	second, secondState, err := ApplyCodexOfficialClientResponsesProfile(profile, "auth-1", []byte(`{"prompt_cache_key":"api-key-shaped-b","input":[{"role":"user","content":"different"}]}`), nil, metadata)
	if err != nil {
		t.Fatalf("second request error = %v", err)
	}
	if firstState.InstallationID != secondState.InstallationID || firstState.SessionID != secondState.SessionID {
		t.Fatal("installation/session identity changed within one logical session")
	}
	if firstState.TurnID == secondState.TurnID {
		t.Fatal("turn identity was reused across requests")
	}
	if gjson.GetBytes(first, "prompt_cache_key").String() != gjson.GetBytes(second, "prompt_cache_key").String() {
		t.Fatal("inbound prompt_cache_key affected profile session identity")
	}

	_, thirdState, err := ApplyCodexOfficialClientResponsesProfile(profile, "auth-1", []byte(`{"input":[{"role":"user","content":"hello"}]}`), nil, map[string]any{cliproxyexecutor.ExecutionSessionMetadataKey: "session-b"})
	if err != nil {
		t.Fatalf("third request error = %v", err)
	}
	if thirdState.InstallationID != firstState.InstallationID || thirdState.SessionID == firstState.SessionID {
		t.Fatal("installation/session lifecycle relation mismatch")
	}
}

func TestApplyCodexOfficialClientCompactProfileUsesAllowlist(t *testing.T) {
	profile, _ := officialclient.LookupProfile("codex-desktop-0.145.0-alpha.18-v1")
	payload := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[{"role":"system","content":"keep compact input as-is"}],
		"instructions":"keep",
		"tools":[],
		"parallel_tool_calls":false,
		"reasoning":{"effort":"high"},
		"text":{"verbosity":"medium"},
		"previous_response_id":"resp-1",
		"stream":true,
		"store":false,
		"prompt_cache_key":"remove",
		"client_metadata":{"remove":true},
		"service_tier":"priority"
	}`)
	body, state, err := ApplyCodexOfficialClientCompactProfile(profile, "auth-1", payload, nil, nil)
	if err != nil {
		t.Fatalf("ApplyCodexOfficialClientCompactProfile() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode compact body: %v", err)
	}
	wantKeys := []string{"input", "instructions", "model", "parallel_tool_calls", "previous_response_id", "reasoning", "text", "tools"}
	gotKeys := make([]string, 0, len(decoded))
	for _, name := range wantKeys {
		if _, ok := decoded[name]; !ok {
			t.Fatalf("compact field %q was removed: %s", name, body)
		}
		gotKeys = append(gotKeys, name)
	}
	if len(decoded) != len(wantKeys) || len(gotKeys) != len(wantKeys) {
		t.Fatalf("compact body has unexpected fields: %s", body)
	}
	if gjson.GetBytes(body, "input.0.role").String() != "system" || gjson.GetBytes(body, "input.0.type").Exists() {
		t.Fatalf("compact body received Responses normalization: %s", body)
	}
	if state == nil || state.SessionID == "" {
		t.Fatal("compact header identity state is missing")
	}
}

func TestCodexOfficialClientHeadersAndProtectedFinalization(t *testing.T) {
	profile, _ := officialclient.LookupProfile("codex-desktop-0.145.0-alpha.18-v1")
	body, state, err := ApplyCodexOfficialClientResponsesProfile(profile, "auth-1", []byte(`{"input":[{"role":"user","content":"hello"}]}`), nil, nil)
	if err != nil {
		t.Fatalf("apply profile error = %v", err)
	}
	desired, err := CodexOfficialClientHeaders(profile, CodexOfficialClientResponses, "sk-account", state)
	if err != nil {
		t.Fatalf("CodexOfficialClientHeaders() error = %v", err)
	}
	headers := http.Header{
		"authorization":                          {"Bearer overridden"},
		"Originator":                             {"other"},
		"session_id":                             {"legacy"},
		"OpenAI-Beta":                            {"responses_websockets=2026-02-06"},
		"X-OpenAI-Internal-Codex-Responses-Lite": {"true"},
		"X-Custom":                               {"keep"},
	}
	FinalizeOfficialClientHeaders(headers, "codex", desired)
	if got := headers.Get("Authorization"); got != "Bearer sk-account" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := headers.Get("User-Agent"); !strings.HasPrefix(got, "Codex Desktop/0.145.0-alpha.18") {
		t.Fatalf("User-Agent = %q", got)
	}
	if headers.Get("X-Api-Key") != "" || headers.Get("OpenAI-Beta") != "" || headers.Get("session_id") != "" || headers.Get("X-OpenAI-Internal-Codex-Responses-Lite") != "" {
		t.Fatalf("removed headers survived: %#v", headers)
	}
	if headers.Get("X-Custom") != "keep" {
		t.Fatalf("custom header was removed: %#v", headers)
	}
	if headers.Get("Session-Id") != state.SessionID || headers.Get("Thread-Id") != state.ThreadID || headers.Get("X-Codex-Turn-Metadata") != state.TurnMetadata {
		t.Fatalf("dynamic header identity mismatch: %#v", headers)
	}
	if gjson.GetBytes(body, "client_metadata.x-codex-turn-metadata").String() != headers.Get("X-Codex-Turn-Metadata") {
		t.Fatal("Header and body turn metadata differ")
	}
}

func assertCodexOfficialClientIdentityRelations(t *testing.T, body []byte, state *CodexOfficialClientRequestState) {
	t.Helper()
	if state == nil || state.InstallationID == "" || state.SessionID == "" || state.TurnID == "" || state.TurnStartedAtUnixMS <= 0 {
		t.Fatalf("request state is incomplete: %#v", state)
	}
	if state.ThreadID != state.SessionID || state.WindowID != state.SessionID+":0" {
		t.Fatalf("request state relation mismatch: %#v", state)
	}
	metadata := gjson.GetBytes(body, "client_metadata")
	want := map[string]string{
		"session_id":              state.SessionID,
		"thread_id":               state.ThreadID,
		"turn_id":                 state.TurnID,
		"x-codex-installation-id": state.InstallationID,
		"x-codex-turn-metadata":   state.TurnMetadata,
		"x-codex-window-id":       state.WindowID,
	}
	got := make(map[string]string, len(want))
	for name := range want {
		got[name] = metadata.Get(name).String()
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("client metadata = %#v, want %#v", got, want)
	}
	turn := gjson.Parse(state.TurnMetadata)
	if turn.Get("installation_id").String() != state.InstallationID || turn.Get("session_id").String() != state.SessionID || turn.Get("thread_id").String() != state.ThreadID || turn.Get("turn_id").String() != state.TurnID || turn.Get("window_id").String() != state.WindowID || turn.Get("turn_started_at_unix_ms").Int() != state.TurnStartedAtUnixMS {
		t.Fatalf("turn metadata relation mismatch: %s", state.TurnMetadata)
	}
	for name, value := range map[string]string{"request_kind": "turn", "sandbox": "none", "thread_source": "user", "workspace_kind": "project"} {
		if got := turn.Get(name).String(); got != value {
			t.Fatalf("turn metadata %s = %q, want %q", name, got, value)
		}
	}
}
