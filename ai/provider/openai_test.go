package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/stream"
)

func TestOpenAIClientStreamText(t *testing.T) {
	client := newHTTPTestClient(func(r *http.Request) (*http.Response, error) {
		rec := struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}{}
		if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
			t.Fatalf("bad request decode: %v", err)
		}
		if rec.Model != "gpt-4o-mini" {
			t.Fatalf("unexpected model: %v", rec.Model)
		}
		if !rec.Stream {
			t.Fatal("expected stream=true")
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("missing auth header: %s", got)
		}

		sse := strings.Join([]string{
			"data: {\"model\":\"gpt-4o-mini\",\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}",
			"",
			"data: {\"choices\":[{\"delta\":{\"content\":\" from OpenAI\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}",
			"",
			"data: [DONE]",
			"",
		}, "\n")
		return sseResponse(sse), nil
	})

	evStream, err := client.Stream(context.Background(), model.Model{
		Provider: "openai",
		ID:       "gpt-4o-mini",
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
	if assistant.Model != "gpt-4o-mini" {
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

func TestOpenAIClientStreamToolCall(t *testing.T) {
	client := newHTTPTestClient(func(r *http.Request) (*http.Response, error) {
		sse := strings.Join([]string{
			"data: {\"model\":\"gpt-4o-mini\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"\"}}]},\"finish_reason\":null}]}",
			"",
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"README.md\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}",
			"",
			"data: [DONE]",
			"",
		}, "\n")
		return sseResponse(sse), nil
	})

	evStream, err := client.Stream(context.Background(), model.Model{
		Provider: "openai",
		ID:       "gpt-4o-mini",
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

func TestOpenAIClientStreamValidation(t *testing.T) {
	t.Run("api key required", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "")
		client := NewOpenAIClient()
		_, err := client.Stream(context.Background(), model.Model{
			Provider: "openai",
			ID:       "gpt-4o-mini",
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
		ID:       "gpt-4o-mini",
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
			"model":"gpt-4o-mini",
			"choices":[
				{
					"finish_reason":"stop",
					"message":{"content":"hello from json","tool_calls":[]}
				}
			],
			"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}
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
		ID:       "gpt-4o-mini",
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

func TestMapStopReason(t *testing.T) {
	tests := []struct {
		in   string
		want model.StopReason
	}{
		{in: "length", want: model.StopReasonLength},
		{in: "tool_calls", want: model.StopReasonToolUse},
		{in: "function_call", want: model.StopReasonToolUse},
		{in: "content_filter", want: model.StopReasonError},
		{in: "other", want: model.StopReasonStop},
	}
	for _, tc := range tests {
		if got := mapStopReason(tc.in); got != tc.want {
			t.Fatalf("mapStopReason(%q): got=%s want=%s", tc.in, got, tc.want)
		}
	}
}

func TestExtractOpenAIMessageText(t *testing.T) {
	text := extractOpenAIMessageText([]any{
		map[string]any{"type": "text", "text": "line1"},
		map[string]any{"type": "text", "text": "line2"},
		map[string]any{"type": "ignored", "text": "nope"},
	})
	if text != "line1\nline2" {
		t.Fatalf("unexpected extracted text: %q", text)
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
