package helps

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/officialclient"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
)

func TestApplyClaudeOfficialClientMessagesProfile(t *testing.T) {
	profile, _ := officialclient.CurrentProfile("claude")
	payload := []byte(`{
		"model":"claude-test",
		"stream":false,
		"system":[
			{"type":"text","text":"x-anthropic-billing-header: cc_version=old; cc_entrypoint=cli; cch=00000;"},
			{"type":"text","text":"You are a Claude agent, built on Anthropic's Claude Agent SDK."},
			"Keep this user instruction"
		],
		"messages":[
			{"role":"user","content":[{"type":"text","text":"run it"},{"type":"tool_use","name":"edit","id":"old"}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"old","content":[{"type":"tool_reference","tool_name":"websearch"}]}]}
		],
		"tool_choice":{"type":"auto"},
		"tools":[
			{"name":"bash","input_schema":{"type":"object"}},
			{"name":"Bash","input_schema":{"type":"object"}},
			{"name":"bAsH","input_schema":{"type":"object"}},
			{"type":"web_search_20250305","name":"read"},
			{"type":"custom","name":"write"}
		]
	}`)
	metadata := map[string]any{cliproxyexecutor.ExecutionSessionMetadataKey: "logical-session"}
	body, state, err := ApplyClaudeOfficialClientMessagesProfile(profile, "auth-1", payload, true, nil, metadata)
	if err != nil {
		t.Fatalf("ApplyClaudeOfficialClientMessagesProfile() error = %v", err)
	}
	if !gjson.GetBytes(body, "stream").Bool() {
		t.Fatalf("stream was not enabled: %s", body)
	}
	if gjson.GetBytes(body, "tool_choice").Exists() {
		t.Fatalf("plain auto tool_choice was not removed: %s", body)
	}
	if got := gjson.GetBytes(body, "tools.0.name").String(); got != "Bash" {
		t.Fatalf("tools.0.name = %q, want Bash", got)
	}
	if got := gjson.GetBytes(body, "tools.1.name").String(); got != "Bash" {
		t.Fatalf("tools.1.name = %q, want unchanged Bash", got)
	}
	if got := gjson.GetBytes(body, "tools.2.name").String(); got != "bAsH" {
		t.Fatalf("tools.2.name = %q, want unchanged bAsH", got)
	}
	if got := gjson.GetBytes(body, "tools.3.name").String(); got != "read" {
		t.Fatalf("typed tool name = %q, want unchanged read", got)
	}
	if got := gjson.GetBytes(body, "tools.4.name").String(); got != "Write" {
		t.Fatalf("typed custom tool name = %q, want Write", got)
	}
	if got := gjson.GetBytes(body, "messages.0.content.1.name").String(); got != "Edit" {
		t.Fatalf("message tool name = %q, want Edit", got)
	}
	if got := gjson.GetBytes(body, "messages.1.content.0.content.0.tool_name").String(); got != "WebSearch" {
		t.Fatalf("nested tool reference = %q, want WebSearch", got)
	}
	if got := gjson.GetBytes(body, "system.#").Int(); got != 3 {
		t.Fatalf("system block count = %d, want 3; body=%s", got, body)
	}
	if got := gjson.GetBytes(body, "system.0.text").String(); !strings.Contains(got, "cc_version=2.1.215.") || !strings.Contains(got, "cc_entrypoint=claude-desktop-3p;") || strings.Contains(got, "cch=") {
		t.Fatalf("billing block = %q", got)
	}
	if got := gjson.GetBytes(body, "system.1.text").String(); got != profile.Claude.IdentitySystemText {
		t.Fatalf("identity block = %q", got)
	}
	if got := gjson.GetBytes(body, "system.2.text").String(); got != "Keep this user instruction" {
		t.Fatalf("user system block = %q", got)
	}
	userID := gjson.GetBytes(body, "metadata.user_id").String()
	if !gjson.Valid(userID) || gjson.Get(userID, "session_id").String() != state.SessionID || len(gjson.Get(userID, "device_id").String()) != 64 {
		t.Fatalf("metadata.user_id = %q, session = %q", userID, state.SessionID)
	}

	body2, state2, err := ApplyClaudeOfficialClientMessagesProfile(profile, "auth-1", payload, true, nil, metadata)
	if err != nil {
		t.Fatal(err)
	}
	if state2.SessionID != state.SessionID || gjson.GetBytes(body2, "metadata.user_id").String() != userID {
		t.Fatal("same logical session did not produce stable identity")
	}
	_, state3, err := ApplyClaudeOfficialClientMessagesProfile(profile, "auth-2", payload, true, nil, metadata)
	if err != nil {
		t.Fatal(err)
	}
	if state3.SessionID == state.SessionID {
		t.Fatal("different auth IDs produced the same session identity")
	}
}

func TestClaudeOfficialClientMessagesPolicies(t *testing.T) {
	profile, _ := officialclient.CurrentProfile("claude")
	payload := []byte(`{"model":"claude-test","stream":true,"messages":[{"role":"user","content":"hello"}],"tool_choice":{"type":"auto","disable_parallel_tool_use":true}}`)
	headers := http.Header{"X-Claude-Code-Session-Id": {"inbound-session"}}
	body, state, err := ApplyClaudeOfficialClientMessagesProfile(profile, "auth-1", payload, false, headers, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(body, "stream").Exists() {
		t.Fatalf("non-stream body retained stream: %s", body)
	}
	if !gjson.GetBytes(body, "tool_choice.disable_parallel_tool_use").Bool() {
		t.Fatalf("tool_choice with extra semantics was removed: %s", body)
	}
	if state.SessionID == "inbound-session" {
		t.Fatal("inbound session ID was reused instead of serving as a seed")
	}

	desired, err := ClaudeOfficialClientHeaders(profile, ClaudeOfficialClientMessagesNonStream, "secret-key", body, state)
	if err != nil {
		t.Fatal(err)
	}
	if desired["x-api-key"] != "secret-key" || desired["Authorization"] != "Bearer secret-key" || desired["X-Claude-Code-Session-Id"] != state.SessionID || desired["X-Stainless-Timeout"] != "900" {
		t.Fatalf("dynamic headers = %#v", desired)
	}
	if got := desired["Anthropic-Beta"]; got != strings.Join(profile.Claude.MessagesNonStreamBetas, ",") {
		t.Fatalf("Anthropic-Beta = %q", got)
	}
	upstreamHeaders := http.Header{
		"Authorization":             {"Bearer old"},
		"X-Client-Request-Id":       {"old"},
		"X-Stainless-Helper-Method": {"old"},
		"X-Custom":                  {"keep"},
	}
	FinalizeOfficialClientHeaders(upstreamHeaders, "claude", desired)
	if upstreamHeaders.Get("Authorization") != "Bearer secret-key" || upstreamHeaders.Get("X-Client-Request-Id") != "" || upstreamHeaders.Get("X-Stainless-Helper-Method") != "" {
		t.Fatalf("removed protected headers survived: %#v", upstreamHeaders)
	}
	if upstreamHeaders.Get("X-Custom") != "keep" || upstreamHeaders.Get("x-api-key") != "secret-key" {
		t.Fatalf("finalized headers = %#v", upstreamHeaders)
	}
}

func TestClaudeOfficialClientStreamHeaders(t *testing.T) {
	profile, _ := officialclient.CurrentProfile("claude")
	body, state, err := ApplyClaudeOfficialClientMessagesProfile(profile, "auth-1", []byte(`{"output_config":{"format":{"type":"json_schema"}},"messages":[{"role":"user","content":"hello"}]}`), true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	desired, err := ClaudeOfficialClientHeaders(profile, ClaudeOfficialClientMessagesStream, "secret-key", body, state)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string(nil), profile.Claude.MessagesStreamBetas...)
	want[len(want)-1] = profile.Claude.StructuredOutputsBeta
	if got := desired["Anthropic-Beta"]; got != strings.Join(want, ",") {
		t.Fatalf("Anthropic-Beta = %q, want %q", got, strings.Join(want, ","))
	}
	if strings.Contains(desired["Anthropic-Beta"], "fallback-credit-2026-06-01") {
		t.Fatalf("structured outputs retained fallback beta: %q", desired["Anthropic-Beta"])
	}
	nonStreamDesired, err := ClaudeOfficialClientHeaders(profile, ClaudeOfficialClientMessagesNonStream, "secret-key", body, state)
	if err != nil {
		t.Fatal(err)
	}
	if got := nonStreamDesired["Anthropic-Beta"]; got != strings.Join(want, ",") {
		t.Fatalf("non-stream Anthropic-Beta = %q, want %q", got, strings.Join(want, ","))
	}
}

func TestApplyClaudeOfficialClientCountTokensProfile(t *testing.T) {
	profile, _ := officialclient.CurrentProfile("claude")
	payload := []byte(`{
		"model":"claude-test",
		"messages":[{"role":"user","content":"hello"}],
		"temperature":1,"top_p":0.9,"top_k":10,"stream":true,"stop_sequences":["x"],"stop":"y",
		"tools":[{"name":"write","input_schema":{"type":"object"}}]
	}`)
	body, state, err := ApplyClaudeOfficialClientCountTokensProfile(profile, "auth-1", payload, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"temperature", "top_p", "top_k", "stream", "stop_sequences", "stop"} {
		if gjson.GetBytes(body, path).Exists() {
			t.Fatalf("count_tokens retained %q: %s", path, body)
		}
	}
	for _, path := range []string{"metadata", "output_config", "thinking"} {
		if gjson.GetBytes(body, path).Exists() {
			t.Fatalf("count_tokens injected %q: %s", path, body)
		}
	}
	if got := gjson.GetBytes(body, "tools.0.name").String(); got != "Write" {
		t.Fatalf("tool name = %q, want Write", got)
	}
	desired, err := ClaudeOfficialClientHeaders(profile, ClaudeOfficialClientCountTokens, "secret-key", body, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := desired["X-Stainless-Timeout"]; ok {
		t.Fatalf("count_tokens contains timeout: %#v", desired)
	}
	if got := desired["Anthropic-Beta"]; got != strings.Join(profile.Claude.CountTokensBetas, ",") {
		t.Fatalf("Anthropic-Beta = %q", got)
	}
	if _, ok := desired["Authorization"]; ok {
		t.Fatalf("count_tokens contains Authorization: %#v", desired)
	}
}

func TestClaudeOfficialClientToolNameRestore(t *testing.T) {
	profile, _ := officialclient.CurrentProfile("claude")
	payload := []byte(`{"messages":[{"role":"user","content":"hello"}],"tools":[{"name":"edit"},{"name":"Bash"},{"name":"custom"}]}`)
	_, state, err := ApplyClaudeOfficialClientMessagesProfile(profile, "auth-1", payload, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := []byte(`{"content":[{"type":"tool_use","name":"Edit","input":{}},{"type":"tool_use","name":"Bash","input":{}},{"type":"text","text":"Edit must stay text"},{"type":"tool_reference","tool_name":"Edit"}]}`)
	restored := state.RestoreJSON(response)
	if gjson.GetBytes(restored, "content.0.name").String() != "edit" || gjson.GetBytes(restored, "content.1.name").String() != "Bash" || gjson.GetBytes(restored, "content.3.tool_name").String() != "edit" {
		t.Fatalf("restored JSON = %s", restored)
	}
	if got := gjson.GetBytes(restored, "content.2.text").String(); got != "Edit must stay text" {
		t.Fatalf("plain text was rewritten: %q", got)
	}
	sse := state.RestoreSSELine([]byte(`data: {"type":"content_block_start","content_block":{"type":"tool_use","name":"Edit"}}`))
	if got := gjson.GetBytes(JSONPayload(sse), "content_block.name").String(); got != "edit" {
		t.Fatalf("restored SSE = %s", sse)
	}
	unchanged := []byte(`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Edit"}}`)
	if got := state.RestoreSSELine(unchanged); !reflect.DeepEqual(got, unchanged) {
		t.Fatalf("plain SSE changed from %s to %s", unchanged, got)
	}
}

func TestOfficialClientHelpersFailRequestScoped(t *testing.T) {
	profile, _ := officialclient.CurrentProfile("claude")
	_, _, err := ApplyClaudeOfficialClientMessagesProfile(profile, "", []byte(`{}`), true, nil, nil)
	if err == nil {
		t.Fatal("missing auth ID unexpectedly succeeded")
	}
	requestScoped, ok := err.(interface{ IsRequestScoped() bool })
	if !ok || !requestScoped.IsRequestScoped() {
		t.Fatalf("error is not request scoped: %T %v", err, err)
	}
	sentinel := errors.New("sentinel")
	if wrapped := NewOfficialClientRequestError(sentinel); !errors.Is(wrapped, sentinel) {
		t.Fatalf("wrapped error does not preserve errors.Is: %v", wrapped)
	}
	if !OfficialClientConnectivityTest(map[string]any{cliproxyexecutor.ConnectivityTestMetadataKey: "true"}) || OfficialClientConnectivityTest(map[string]any{cliproxyexecutor.ConnectivityTestMetadataKey: "invalid"}) {
		t.Fatal("connectivity metadata parsing is incorrect")
	}
}
