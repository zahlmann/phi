package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/stream"
)

func TestOpenAIClientStreamText(t *testing.T) {
	client := newHTTPTestClient(func(r *http.Request) (*http.Response, error) {
		if got := r.URL.String(); got != "https://example.invalid/v1/responses" {
			t.Fatalf("unexpected request url: %s", got)
		}

		req := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("bad request decode: %v", err)
		}
		if req["model"] != "gpt-5.2-codex" {
			t.Fatalf("unexpected model: %v", req["model"])
		}
		if req["stream"] != true {
			t.Fatal("expected stream=true")
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("missing auth header: %s", got)
		}

		sse := strings.Join([]string{
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}",
			"",
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\" from OpenAI\"}",
			"",
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_text\",\"model\":\"gpt-5.2-codex\",\"usage\":{\"input_tokens\":10,\"output_tokens\":5,\"total_tokens\":15}}}",
			"",
		}, "\n")
		return sseResponse(sse), nil
	})

	evStream, err := client.Stream(context.Background(), model.Model{
		Provider: "openai",
		ID:       "gpt-5.2-codex",
	}, model.Context{
		SystemPrompt: "You are helpful",
		Messages: []model.Message{
			{
				Role: model.RoleUser,
				ContentRaw: []any{
					model.TextContent{Type: model.ContentText, Text: "Hi"},
				},
			},
		},
	}, StreamOptions{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	seenTextDelta := false
	for {
		ev, recvErr := evStream.Recv()
		if recvErr != nil {
			break
		}
		if ev.Type == stream.EventTextDelta {
			seenTextDelta = true
		}
	}
	if !seenTextDelta {
		t.Fatal("expected text delta event")
	}

	assistant, err := evStream.Result()
	if err != nil {
		t.Fatalf("result failed: %v", err)
	}
	if assistant.Model != "gpt-5.2-codex" {
		t.Fatalf("unexpected model: %s", assistant.Model)
	}
	if assistant.Usage.Total != 15 {
		t.Fatalf("unexpected usage: %d", assistant.Usage.Total)
	}
	text := extractText(assistant.ContentRaw)
	if text != "Hello from OpenAI" {
		t.Fatalf("unexpected text: %q", text)
	}
}

func TestOpenAIClientStreamCodexUsesResponsesAPI(t *testing.T) {
	client := newHTTPTestClient(func(r *http.Request) (*http.Response, error) {
		if got := r.URL.String(); got != "https://example.invalid/v1/responses" {
			t.Fatalf("unexpected request url: %s", got)
		}

		req := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req["model"] != "gpt-5.2-codex" {
			t.Fatalf("unexpected model in request: %#v", req["model"])
		}
		reasoning, _ := req["reasoning"].(map[string]any)
		if reasoning["effort"] != "none" {
			t.Fatalf("unexpected reasoning payload: %#v", req["reasoning"])
		}

		sse := strings.Join([]string{
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}",
			"",
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_codex\",\"model\":\"gpt-5.2-codex\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}",
			"",
		}, "\n")
		return sseResponse(sse), nil
	})

	evStream, err := client.Stream(context.Background(), model.Model{
		Provider: "openai",
		ID:       "gpt-5.2-codex",
	}, model.Context{
		Messages: []model.Message{
			{
				Role: model.RoleUser,
				ContentRaw: []any{
					model.TextContent{Type: model.ContentText, Text: "Hi"},
				},
			},
		},
	}, StreamOptions{
		APIKey:          "test-key",
		ReasoningEffort: "none",
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	for {
		_, recvErr := evStream.Recv()
		if recvErr != nil {
			break
		}
	}

	assistant, err := evStream.Result()
	if err != nil {
		t.Fatalf("result failed: %v", err)
	}
	if assistant.Provider != "openai" {
		t.Fatalf("unexpected provider: %s", assistant.Provider)
	}
	if got := extractText(assistant.ContentRaw); got != "OK" {
		t.Fatalf("unexpected assistant text: %q", got)
	}
}

func TestNormalizeReasoningEffort(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "none", want: "none"},
		{in: "low", want: "low"},
		{in: "medium", want: "medium"},
		{in: "high", want: "high"},
		{in: "xhigh", want: "xhigh"},
		{in: "minimal", want: "minimal"},
		{in: "bogus", want: ""},
	}
	for _, tc := range tests {
		if got := normalizeReasoningEffort(tc.in); got != tc.want {
			t.Fatalf("normalizeReasoningEffort(%q): got=%q want=%q", tc.in, got, tc.want)
		}
	}
}

func TestOpenAIClientStreamToolCall(t *testing.T) {
	client := newHTTPTestClient(func(r *http.Request) (*http.Response, error) {
		sse := strings.Join([]string{
			"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}",
			"",
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_tool\",\"model\":\"gpt-5.2-codex\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}",
			"",
		}, "\n")
		return sseResponse(sse), nil
	})

	evStream, err := client.Stream(context.Background(), model.Model{
		Provider: "openai",
		ID:       "gpt-5.2-codex",
	}, model.Context{
		Messages: []model.Message{
			{
				Role: model.RoleUser,
				ContentRaw: []any{
					model.TextContent{Type: model.ContentText, Text: "Read README"},
				},
			},
		},
	}, StreamOptions{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	toolEventSeen := false
	for {
		ev, recvErr := evStream.Recv()
		if recvErr != nil {
			break
		}
		if ev.Type == stream.EventToolCall {
			toolEventSeen = true
		}
	}
	if !toolEventSeen {
		t.Fatal("expected tool call event")
	}

	assistant, err := evStream.Result()
	if err != nil {
		t.Fatalf("result failed: %v", err)
	}
	if assistant.StopReason != model.StopReasonToolUse {
		t.Fatalf("unexpected stop reason: %s", assistant.StopReason)
	}
	if len(assistant.ContentRaw) != 1 {
		t.Fatalf("expected one content block, got %d", len(assistant.ContentRaw))
	}
	call, ok := assistant.ContentRaw[0].(model.ToolCallContent)
	if !ok {
		t.Fatalf("expected tool call content, got %T", assistant.ContentRaw[0])
	}
	if call.Name != "read_file" {
		t.Fatalf("unexpected tool name: %s", call.Name)
	}
	if call.Arguments["path"] != "README.md" {
		t.Fatalf("unexpected tool args: %#v", call.Arguments)
	}
}

func TestOpenAIClientStreamChatGPTBackendText(t *testing.T) {
	client := newHTTPTestClient(func(r *http.Request) (*http.Response, error) {
		if got := r.URL.String(); got != "https://chatgpt.com/backend-api/codex/responses" {
			t.Fatalf("unexpected request url: %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer chatgpt-token" {
			t.Fatalf("unexpected auth header: %s", got)
		}
		if got := r.Header.Get("ChatGPT-Account-ID"); got != "acc_123" {
			t.Fatalf("unexpected account id header: %s", got)
		}

		req := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req["model"] != "gpt-5.3-codex" {
			t.Fatalf("unexpected model in request: %#v", req["model"])
		}
		if req["stream"] != true {
			t.Fatalf("expected stream=true, got %#v", req["stream"])
		}
		if req["store"] != false {
			t.Fatalf("expected store=false, got %#v", req["store"])
		}

		sse := strings.Join([]string{
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}",
			"",
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\" from ChatGPT\"}",
			"",
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-5.3-codex\",\"usage\":{\"input_tokens\":11,\"output_tokens\":7,\"total_tokens\":18}}}",
			"",
		}, "\n")
		return sseResponse(sse), nil
	})

	evStream, err := client.Stream(context.Background(), model.Model{
		Provider: "openai",
		ID:       "gpt-5.3-codex",
	}, model.Context{
		Messages: []model.Message{
			{
				Role: model.RoleUser,
				ContentRaw: []any{
					model.TextContent{Type: model.ContentText, Text: "Hi"},
				},
			},
		},
	}, StreamOptions{
		AuthMode:    AuthModeChatGPT,
		AccessToken: "chatgpt-token",
		AccountID:   "acc_123",
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	seenTextDelta := false
	for {
		ev, recvErr := evStream.Recv()
		if recvErr != nil {
			break
		}
		if ev.Type == stream.EventTextDelta {
			seenTextDelta = true
		}
	}
	if !seenTextDelta {
		t.Fatal("expected text delta event")
	}

	assistant, err := evStream.Result()
	if err != nil {
		t.Fatalf("result failed: %v", err)
	}
	if assistant.Provider != "chatgpt" {
		t.Fatalf("unexpected provider: %s", assistant.Provider)
	}
	if assistant.Usage.Total != 18 {
		t.Fatalf("unexpected usage: %#v", assistant.Usage)
	}
	if got := extractText(assistant.ContentRaw); got != "Hello from ChatGPT" {
		t.Fatalf("unexpected assistant text: %q", got)
	}
}

func TestOpenAIClientStreamChatGPTBackendToolCall(t *testing.T) {
	client := newHTTPTestClient(func(r *http.Request) (*http.Response, error) {
		sse := strings.Join([]string{
			"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}",
			"",
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_2\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}",
			"",
		}, "\n")
		return sseResponse(sse), nil
	})

	evStream, err := client.Stream(context.Background(), model.Model{
		Provider: "openai",
		ID:       "gpt-5.3-codex",
	}, model.Context{
		Messages: []model.Message{
			{
				Role: model.RoleUser,
				ContentRaw: []any{
					model.TextContent{Type: model.ContentText, Text: "Read README"},
				},
			},
		},
	}, StreamOptions{
		AuthMode:    AuthModeChatGPT,
		AccessToken: "chatgpt-token",
		AccountID:   "acc_123",
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	toolEventSeen := false
	for {
		ev, recvErr := evStream.Recv()
		if recvErr != nil {
			break
		}
		if ev.Type == stream.EventToolCall {
			toolEventSeen = true
		}
	}
	if !toolEventSeen {
		t.Fatal("expected tool call event")
	}

	assistant, err := evStream.Result()
	if err != nil {
		t.Fatalf("result failed: %v", err)
	}
	if assistant.StopReason != model.StopReasonToolUse {
		t.Fatalf("unexpected stop reason: %s", assistant.StopReason)
	}
	if len(assistant.ContentRaw) != 1 {
		t.Fatalf("expected one content block, got %d", len(assistant.ContentRaw))
	}
	call, ok := assistant.ContentRaw[0].(model.ToolCallContent)
	if !ok {
		t.Fatalf("expected tool call content, got %T", assistant.ContentRaw[0])
	}
	if call.Name != "read_file" {
		t.Fatalf("unexpected tool name: %s", call.Name)
	}
	if call.Arguments["path"] != "README.md" {
		t.Fatalf("unexpected tool args: %#v", call.Arguments)
	}
}

func TestOpenAIClientStreamChatGPTBackendTreatsTextPlainAsSSE(t *testing.T) {
	client := newHTTPTestClient(func(*http.Request) (*http.Response, error) {
		sse := strings.Join([]string{
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}",
			"",
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_3\",\"model\":\"gpt-5.3-codex\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}",
			"",
		}, "\n")
		header := make(http.Header)
		header.Set("Content-Type", "text/plain; charset=utf-8")
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(sse)),
			Header:     header,
		}, nil
	})

	evStream, err := client.Stream(context.Background(), model.Model{
		Provider: "openai",
		ID:       "gpt-5.3-codex",
	}, model.Context{
		Messages: []model.Message{
			{
				Role: model.RoleUser,
				ContentRaw: []any{
					model.TextContent{Type: model.ContentText, Text: "Hi"},
				},
			},
		},
	}, StreamOptions{
		AuthMode:    AuthModeChatGPT,
		AccessToken: "chatgpt-token",
		AccountID:   "acc_123",
	})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	for {
		_, recvErr := evStream.Recv()
		if recvErr != nil {
			break
		}
	}

	assistant, err := evStream.Result()
	if err != nil {
		t.Fatalf("result failed: %v", err)
	}
	if got := extractText(assistant.ContentRaw); got != "hello" {
		t.Fatalf("unexpected assistant text: %q", got)
	}
}

func TestChatGPTStreamIgnoresCloseErrorAfterCompleted(t *testing.T) {
	sse := strings.Join([]string{
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}",
		"",
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-5.3-codex\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}",
		"",
	}, "\n")
	chunks := [][]byte{
		[]byte(sse[:len(sse)/2]),
		[]byte(sse[len(sse)/2:]),
	}

	ctx := context.Background()
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       &errorAfterChunksReadCloser{chunks: chunks, err: context.Canceled},
	}

	evStream := newChatGPTResponsesEventStream(reqCtx, cancel, resp, model.Model{
		Provider: "openai",
		ID:       "gpt-5.3-codex",
	}, "chatgpt")

	for {
		_, recvErr := evStream.Recv()
		if recvErr != nil {
			break
		}
	}

	assistant, err := evStream.Result()
	if err != nil {
		t.Fatalf("expected success after completed event, got %v", err)
	}
	if assistant == nil {
		t.Fatal("expected assistant message")
	}
	if got := extractText(assistant.ContentRaw); got != "ok" {
		t.Fatalf("unexpected assistant text: %q", got)
	}
}

func TestOpenAIClientStreamValidation(t *testing.T) {
	t.Run("api key required", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "")
		client := NewOpenAIClient()
		_, err := client.Stream(context.Background(), model.Model{
			Provider: "openai",
			ID:       "gpt-5.2-codex",
		}, model.Context{}, StreamOptions{})
		if err == nil || !strings.Contains(err.Error(), "openai api key is required") {
			t.Fatalf("expected api key validation error, got %v", err)
		}
	})

	t.Run("model id required", func(t *testing.T) {
		client := NewOpenAIClient()
		_, err := client.Stream(context.Background(), model.Model{
			Provider: "openai",
		}, model.Context{}, StreamOptions{APIKey: "test-key"})
		if err == nil || !strings.Contains(err.Error(), "model id is required") {
			t.Fatalf("expected model id validation error, got %v", err)
		}
	})

	t.Run("chatgpt token required", func(t *testing.T) {
		t.Setenv("PHI_CHATGPT_ACCESS_TOKEN", "")
		t.Setenv("PHI_CHATGPT_ACCOUNT_ID", "")
		t.Setenv("HOME", t.TempDir())
		client := NewOpenAIClient()
		_, err := client.Stream(context.Background(), model.Model{
			Provider: "openai",
			ID:       "gpt-5.3-codex",
		}, model.Context{}, StreamOptions{AuthMode: AuthModeChatGPT})
		if err == nil || !strings.Contains(err.Error(), "chatgpt access token is required") {
			t.Fatalf("expected chatgpt token validation error, got %v", err)
		}
	})
}

func TestResolveChatGPTAuthPrecedence(t *testing.T) {
	t.Run("falls back to codex auth by default", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("PHI_CHATGPT_ACCESS_TOKEN", "")
		t.Setenv("PHI_CHATGPT_ACCOUNT_ID", "")
		writeJSONFile(t, filepath.Join(home, ".codex", "auth.json"), map[string]any{
			"tokens": map[string]any{
				"access_token": testJWT("codex-account"),
				"account_id":   "",
			},
		})

		token, accountID, err := resolveChatGPTAuth(context.Background(), StreamOptions{})
		if err != nil {
			t.Fatalf("resolveChatGPTAuth failed: %v", err)
		}
		if token != testJWT("codex-account") {
			t.Fatalf("expected codex token, got %q", token)
		}
		if accountID != "codex-account" {
			t.Fatalf("expected account id from codex token, got %q", accountID)
		}
	})

	t.Run("prefers phi file over codex auth", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("PHI_CHATGPT_ACCESS_TOKEN", "")
		t.Setenv("PHI_CHATGPT_ACCOUNT_ID", "")
		writeJSONFile(t, filepath.Join(home, ".codex", "auth.json"), map[string]any{
			"tokens": map[string]any{
				"access_token": testJWT("codex-account"),
				"account_id":   "",
			},
		})
		writeJSONFile(t, filepath.Join(home, ".phi", "chatgpt_tokens.json"), map[string]any{
			"accessToken": testJWT("phi-account"),
			"accountId":   "phi-account",
		})

		token, accountID, err := resolveChatGPTAuth(context.Background(), StreamOptions{})
		if err != nil {
			t.Fatalf("resolveChatGPTAuth failed: %v", err)
		}
		if token != testJWT("phi-account") {
			t.Fatalf("expected phi token, got %q", token)
		}
		if accountID != "phi-account" {
			t.Fatalf("expected phi account id, got %q", accountID)
		}
	})

	t.Run("prefers env over file sources", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("PHI_CHATGPT_ACCESS_TOKEN", testJWT("env-account"))
		t.Setenv("PHI_CHATGPT_ACCOUNT_ID", "env-account")
		writeJSONFile(t, filepath.Join(home, ".codex", "auth.json"), map[string]any{
			"tokens": map[string]any{
				"access_token": testJWT("codex-account"),
			},
		})
		writeJSONFile(t, filepath.Join(home, ".phi", "chatgpt_tokens.json"), map[string]any{
			"accessToken": testJWT("phi-account"),
			"accountId":   "phi-account",
		})

		token, accountID, err := resolveChatGPTAuth(context.Background(), StreamOptions{})
		if err != nil {
			t.Fatalf("resolveChatGPTAuth failed: %v", err)
		}
		if token != testJWT("env-account") {
			t.Fatalf("expected env token, got %q", token)
		}
		if accountID != "env-account" {
			t.Fatalf("expected env account id, got %q", accountID)
		}
	})

	t.Run("prefers explicit options over env", func(t *testing.T) {
		t.Setenv("PHI_CHATGPT_ACCESS_TOKEN", testJWT("env-account"))
		t.Setenv("PHI_CHATGPT_ACCOUNT_ID", "env-account")

		token, accountID, err := resolveChatGPTAuth(context.Background(), StreamOptions{
			AccessToken: testJWT("option-account"),
			AccountID:   "option-account",
		})
		if err != nil {
			t.Fatalf("resolveChatGPTAuth failed: %v", err)
		}
		if token != testJWT("option-account") {
			t.Fatalf("expected option token, got %q", token)
		}
		if accountID != "option-account" {
			t.Fatalf("expected option account id, got %q", accountID)
		}
	})
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

func testJWT(accountID string) string {
	header, _ := json.Marshal(map[string]any{"alg": "none", "typ": "JWT"})
	payload, _ := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": accountID,
		},
	})
	return base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestOpenAIClientStreamHTTPStatusError(t *testing.T) {
	client := newHTTPTestClient(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 401,
			Body:       io.NopCloser(strings.NewReader("bad token")),
			Header:     make(http.Header),
		}, nil
	})

	_, err := client.Stream(context.Background(), model.Model{
		Provider: "openai",
		ID:       "gpt-5.2-codex",
	}, model.Context{}, StreamOptions{APIKey: "test-key"})
	if err == nil {
		t.Fatal("expected status error")
	}
	if !strings.Contains(err.Error(), "status=401") || !strings.Contains(err.Error(), "bad token") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenAIClientStreamParsesNonStreamingResponse(t *testing.T) {
	client := newHTTPTestClient(func(*http.Request) (*http.Response, error) {
		body := `{
			"id":"resp_nonstream",
			"model":"gpt-5.2-codex",
			"output":[
				{
					"type":"message",
					"content":[{"type":"output_text","text":"hello from json"}]
				}
			],
			"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}
		}`
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     header,
		}, nil
	})

	evStream, err := client.Stream(context.Background(), model.Model{
		Provider: "openai",
		ID:       "gpt-5.2-codex",
	}, model.Context{}, StreamOptions{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}

	var textDelta string
	for {
		ev, recvErr := evStream.Recv()
		if recvErr != nil {
			break
		}
		if ev.Type == stream.EventTextDelta {
			textDelta = ev.Delta
		}
	}
	if textDelta != "hello from json" {
		t.Fatalf("unexpected text delta: %q", textDelta)
	}

	assistant, err := evStream.Result()
	if err != nil {
		t.Fatalf("result failed: %v", err)
	}
	if assistant.Usage.Total != 7 {
		t.Fatalf("unexpected usage: %#v", assistant.Usage)
	}
	if got := extractText(assistant.ContentRaw); got != "hello from json" {
		t.Fatalf("unexpected assistant text: %q", got)
	}
}

func TestConsumeSSE(t *testing.T) {
	body := "data: first\ndata: line\n\n: keep-alive\ndata: second\n\n"
	payloads := []string{}
	err := consumeSSE(strings.NewReader(body), func(payload string) error {
		payloads = append(payloads, payload)
		return nil
	})
	if err != nil {
		t.Fatalf("consumeSSE failed: %v", err)
	}
	want := []string{"first\nline", "second"}
	if !reflect.DeepEqual(payloads, want) {
		t.Fatalf("unexpected payloads: got=%#v want=%#v", payloads, want)
	}
}

func TestParseToolArguments(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want map[string]any
	}{
		{
			name: "json object",
			raw:  `{"path":"README.md"}`,
			want: map[string]any{"path": "README.md"},
		},
		{
			name: "json scalar",
			raw:  `123`,
			want: map[string]any{"value": float64(123)},
		},
		{
			name: "invalid json",
			raw:  `{"path":`,
			want: map[string]any{"_raw": `{"path":`},
		},
		{
			name: "empty",
			raw:  "",
			want: map[string]any{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseToolArguments(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unexpected parsed arguments: got=%#v want=%#v", got, tc.want)
			}
		})
	}
}

func newHTTPTestClient(handler func(*http.Request) (*http.Response, error)) *OpenAIClient {
	client := NewOpenAIClient()
	client.BaseURL = "https://example.invalid/v1"
	client.HTTPClient = &http.Client{
		Transport: roundTripFunc(handler),
	}
	return client
}

func sseResponse(body string) *http.Response {
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     header,
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type errorAfterChunksReadCloser struct {
	chunks [][]byte
	index  int
	err    error
}

func (r *errorAfterChunksReadCloser) Read(p []byte) (int, error) {
	if r.index < len(r.chunks) {
		chunk := r.chunks[r.index]
		r.index++
		n := copy(p, chunk)
		return n, nil
	}
	if r.err != nil {
		err := r.err
		r.err = nil
		return 0, err
	}
	return 0, io.EOF
}

func (r *errorAfterChunksReadCloser) Close() error {
	return nil
}
