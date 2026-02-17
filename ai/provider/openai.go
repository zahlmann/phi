package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/stream"
)

var errSSEDone = errors.New("sse done")

type OpenAIClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewOpenAIClient() *OpenAIClient {
	return &OpenAIClient{
		BaseURL: "https://api.openai.com/v1",
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *OpenAIClient) Stream(
	ctx context.Context,
	m model.Model,
	conversation model.Context,
	options StreamOptions,
) (stream.EventStream, error) {
	apiKey := strings.TrimSpace(options.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if apiKey == "" {
		return nil, errors.New("openai api key is required")
	}
	if m.ID == "" {
		return nil, errors.New("model id is required")
	}

	request := buildOpenAIChatRequest(m, conversation, options)
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	baseURL := strings.TrimRight(options.BaseURL, "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(c.BaseURL, "/")
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	reqCtx, cancel := context.WithCancel(ctx)
	url := baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		cancel()
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range options.Headers {
		httpReq.Header.Set(k, v)
	}

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		cancel()
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("openai request failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "text/event-stream") {
		cancel()
		return parseOpenAINonStreamingResponse(resp, m)
	}

	return newOpenAIEventStream(reqCtx, cancel, resp, m), nil
}

type openAIChatRequest struct {
	Model               string               `json:"model"`
	Messages            []openAIChatMessage  `json:"messages"`
	Tools               []openAIChatTool     `json:"tools,omitempty"`
	ToolChoice          string               `json:"tool_choice,omitempty"`
	Stream              bool                 `json:"stream"`
	StreamOptions       *openAIStreamOptions `json:"stream_options,omitempty"`
	Temperature         *float64             `json:"temperature,omitempty"`
	MaxCompletionTokens int                  `json:"max_completion_tokens,omitempty"`
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIChatTool struct {
	Type     string                 `json:"type"`
	Function openAIChatToolFunction `json:"function"`
}

type openAIChatToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openAIChatMessage struct {
	Role       string               `json:"role"`
	Content    any                  `json:"content,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIChatToolCall `json:"tool_calls,omitempty"`
	Name       string               `json:"name,omitempty"`
}

type openAIChatToolCall struct {
	ID       string                     `json:"id"`
	Type     string                     `json:"type"`
	Function openAIChatToolCallFunction `json:"function"`
}

type openAIChatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func buildOpenAIChatRequest(m model.Model, conversation model.Context, options StreamOptions) openAIChatRequest {
	req := openAIChatRequest{
		Model:         m.ID,
		Messages:      toOpenAIMessages(conversation),
		Stream:        true,
		StreamOptions: &openAIStreamOptions{IncludeUsage: true},
	}
	if options.Temperature != nil {
		req.Temperature = options.Temperature
	}
	if options.MaxTokens > 0 {
		req.MaxCompletionTokens = options.MaxTokens
	}
	if len(conversation.Tools) > 0 {
		req.Tools = convertOpenAITools(conversation.Tools)
		req.ToolChoice = "auto"
	}
	return req
}

func convertOpenAITools(tools []model.Tool) []openAIChatTool {
	out := make([]openAIChatTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, openAIChatTool{
			Type: "function",
			Function: openAIChatToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}
	return out
}

func toOpenAIMessages(conversation model.Context) []openAIChatMessage {
	out := []openAIChatMessage{}
	if strings.TrimSpace(conversation.SystemPrompt) != "" {
		out = append(out, openAIChatMessage{
			Role:    "system",
			Content: conversation.SystemPrompt,
		})
	}

	for _, msg := range conversation.Messages {
		switch msg.Role {
		case model.RoleUser:
			content := extractOpenAIUserContent(msg.ContentRaw)
			if content == nil {
				continue
			}
			out = append(out, openAIChatMessage{
				Role:    "user",
				Content: content,
			})
		case model.RoleAssistant:
			text := extractText(msg.ContentRaw)
			toolCalls := extractToolCalls(msg.ContentRaw)
			if text == "" && len(toolCalls) == 0 {
				continue
			}
			item := openAIChatMessage{Role: "assistant"}
			if text != "" {
				item.Content = text
			}
			if len(toolCalls) > 0 {
				item.ToolCalls = toolCalls
			}
			out = append(out, item)
		case model.RoleToolResult:
			if strings.TrimSpace(msg.ToolCallID) == "" {
				continue
			}
			text := extractText(msg.ContentRaw)
			if text == "" {
				text = "(no content)"
			}
			out = append(out, openAIChatMessage{
				Role:       "tool",
				ToolCallID: msg.ToolCallID,
				Name:       msg.ToolName,
				Content:    text,
			})
		}
	}

	return out
}

func extractOpenAIUserContent(content []any) any {
	hasImage := false
	parts := []map[string]any{}
	textParts := []string{}

	for _, item := range content {
		switch v := item.(type) {
		case model.TextContent:
			if strings.TrimSpace(v.Text) != "" {
				textParts = append(textParts, v.Text)
				parts = append(parts, map[string]any{
					"type": "text",
					"text": v.Text,
				})
			}
		case model.ImageContent:
			if strings.TrimSpace(v.Data) != "" {
				hasImage = true
				parts = append(parts, map[string]any{
					"type": "image_url",
					"image_url": map[string]any{
						"url": "data:" + v.MIMEType + ";base64," + v.Data,
					},
				})
			}
		case map[string]any:
			kind, _ := v["type"].(string)
			switch kind {
			case string(model.ContentText):
				text, _ := v["text"].(string)
				if strings.TrimSpace(text) != "" {
					textParts = append(textParts, text)
					parts = append(parts, map[string]any{
						"type": "text",
						"text": text,
					})
				}
			case string(model.ContentImage):
				mime, _ := v["mimeType"].(string)
				data, _ := v["data"].(string)
				if strings.TrimSpace(data) != "" {
					hasImage = true
					parts = append(parts, map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": "data:" + mime + ";base64," + data,
						},
					})
				}
			}
		}
	}

	if len(parts) == 0 {
		return nil
	}
	if !hasImage {
		return strings.Join(textParts, "\n")
	}
	return parts
}

func extractToolCalls(content []any) []openAIChatToolCall {
	out := []openAIChatToolCall{}
	for i, item := range content {
		call := openAIChatToolCall{Type: "function"}
		found := false

		switch v := item.(type) {
		case model.ToolCallContent:
			call.ID = strings.TrimSpace(v.ID)
			call.Function.Name = strings.TrimSpace(v.Name)
			args, _ := json.Marshal(v.Arguments)
			call.Function.Arguments = string(args)
			found = true
		case map[string]any:
			kind, _ := v["type"].(string)
			if kind == string(model.ContentToolCall) {
				call.ID, _ = v["id"].(string)
				call.Function.Name, _ = v["name"].(string)
				if rawArgs, ok := v["arguments"]; ok {
					args, _ := json.Marshal(rawArgs)
					call.Function.Arguments = string(args)
				}
				found = true
			}
		}

		if !found {
			continue
		}
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%d", i+1)
		}
		if call.Function.Name == "" {
			call.Function.Name = "tool"
		}
		if call.Function.Arguments == "" {
			call.Function.Arguments = "{}"
		}
		out = append(out, call)
	}
	return out
}

func extractText(content []any) string {
	parts := []string{}
	for _, item := range content {
		switch v := item.(type) {
		case model.TextContent:
			if strings.TrimSpace(v.Text) != "" {
				parts = append(parts, v.Text)
			}
		case map[string]any:
			kind, _ := v["type"].(string)
			if kind == string(model.ContentText) {
				text, _ := v["text"].(string)
				if strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

type openAIEventStream struct {
	events    chan openAIEventItem
	result    chan openAIResultItem
	closeFn   func()
	closeOnce sync.Once
}

type openAIEventItem struct {
	event stream.Event
	err   error
}

type openAIResultItem struct {
	msg *model.AssistantMessage
	err error
}

func newOpenAIEventStream(ctx context.Context, cancel context.CancelFunc, resp *http.Response, m model.Model) *openAIEventStream {
	s := &openAIEventStream{
		events: make(chan openAIEventItem, 64),
		result: make(chan openAIResultItem, 1),
		closeFn: func() {
			cancel()
			_ = resp.Body.Close()
		},
	}
	go s.consume(ctx, resp, m)
	return s
}

func (s *openAIEventStream) Recv() (stream.Event, error) {
	item, ok := <-s.events
	if !ok {
		return stream.Event{}, io.EOF
	}
	if item.err != nil {
		return stream.Event{}, item.err
	}
	return item.event, nil
}

func (s *openAIEventStream) Result() (*model.AssistantMessage, error) {
	item, ok := <-s.result
	if !ok {
		return nil, errors.New("stream result unavailable")
	}
	return item.msg, item.err
}

func (s *openAIEventStream) Close() error {
	s.closeOnce.Do(s.closeFn)
	return nil
}

func (s *openAIEventStream) consume(ctx context.Context, resp *http.Response, m model.Model) {
	defer close(s.events)
	defer close(s.result)
	defer resp.Body.Close()

	agg := newOpenAIAggregation(m)
	s.pushEvent(stream.Event{Type: stream.EventStart})

	err := consumeSSE(resp.Body, func(payload string) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if payload == "[DONE]" {
			return errSSEDone
		}

		var chunk openAIChatStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return err
		}
		agg.applyChunk(chunk, s.pushEvent)
		return nil
	})

	if err != nil && !errors.Is(err, errSSEDone) {
		s.pushEvent(stream.Event{
			Type:  stream.EventError,
			Error: err.Error(),
		})
		s.result <- openAIResultItem{err: err}
		return
	}

	calls := agg.finalizeToolCalls()
	for _, call := range calls {
		s.pushEvent(stream.Event{
			Type:       stream.EventToolCall,
			ToolName:   call.Name,
			ToolCallID: call.ID,
			Arguments:  call.Arguments,
		})
	}

	assistant := agg.buildAssistant(calls)
	s.pushEvent(stream.Event{
		Type:   stream.EventDone,
		Reason: assistant.StopReason,
	})
	s.result <- openAIResultItem{msg: assistant}
}

func (s *openAIEventStream) pushEvent(event stream.Event) {
	s.events <- openAIEventItem{event: event}
}

func consumeSSE(body io.Reader, onData func(payload string) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		return onData(payload)
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(trimmed, ":") {
			continue
		}
		if strings.HasPrefix(trimmed, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

type openAIChatStreamChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content   string                    `json:"content"`
			ToolCalls []openAIStreamToolCallRaw `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openAIChatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   any                     `json:"content"`
			ToolCalls []openAIChatToolCallRaw `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openAIChatToolCallRaw struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIStreamToolCallRaw struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIToolCallState struct {
	ID   string
	Name string
	Args strings.Builder
}

type openAIAggregation struct {
	requestModel  model.Model
	responseModel string
	text          strings.Builder
	toolCalls     map[int]*openAIToolCallState
	toolOrder     []int
	usage         model.Usage
	stopReason    model.StopReason
}

func newOpenAIAggregation(m model.Model) *openAIAggregation {
	return &openAIAggregation{
		requestModel: m,
		toolCalls:    map[int]*openAIToolCallState{},
		stopReason:   model.StopReasonStop,
	}
}

func (a *openAIAggregation) applyChunk(chunk openAIChatStreamChunk, emit func(stream.Event)) {
	if chunk.Model != "" {
		a.responseModel = chunk.Model
	}
	if chunk.Usage != nil {
		a.usage = model.Usage{
			Input:  chunk.Usage.PromptTokens,
			Output: chunk.Usage.CompletionTokens,
			Total:  chunk.Usage.TotalTokens,
		}
	}

	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			a.text.WriteString(choice.Delta.Content)
			emit(stream.Event{
				Type:  stream.EventTextDelta,
				Delta: choice.Delta.Content,
			})
		}

		for _, tc := range choice.Delta.ToolCalls {
			call := a.getToolCall(tc.Index)
			if tc.ID != "" {
				call.ID = tc.ID
			}
			if tc.Function.Name != "" {
				call.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				call.Args.WriteString(tc.Function.Arguments)
			}
		}

		if choice.FinishReason != nil && *choice.FinishReason != "" {
			a.stopReason = mapStopReason(*choice.FinishReason)
		}
	}
}

func (a *openAIAggregation) getToolCall(index int) *openAIToolCallState {
	if call, ok := a.toolCalls[index]; ok {
		return call
	}
	call := &openAIToolCallState{}
	a.toolCalls[index] = call
	a.toolOrder = append(a.toolOrder, index)
	return call
}

func (a *openAIAggregation) finalizeToolCalls() []model.ToolCallContent {
	out := make([]model.ToolCallContent, 0, len(a.toolOrder))
	for i, index := range a.toolOrder {
		call := a.toolCalls[index]
		if call == nil {
			continue
		}
		id := strings.TrimSpace(call.ID)
		if id == "" {
			id = fmt.Sprintf("call_%d", i+1)
		}
		name := strings.TrimSpace(call.Name)
		if name == "" {
			name = "tool"
		}
		out = append(out, model.ToolCallContent{
			Type:      model.ContentToolCall,
			ID:        id,
			Name:      name,
			Arguments: parseToolArguments(call.Args.String()),
		})
	}
	return out
}

func (a *openAIAggregation) buildAssistant(calls []model.ToolCallContent) *model.AssistantMessage {
	content := []any{}
	if text := strings.TrimSpace(a.text.String()); text != "" {
		content = append(content, model.TextContent{
			Type: model.ContentText,
			Text: text,
		})
	}
	for _, call := range calls {
		content = append(content, call)
	}

	modelID := a.responseModel
	if modelID == "" {
		modelID = a.requestModel.ID
	}

	return &model.AssistantMessage{
		Role:       model.RoleAssistant,
		ContentRaw: content,
		Provider:   "openai",
		Model:      modelID,
		StopReason: a.stopReason,
		Usage:      a.usage,
		Timestamp:  time.Now().UnixMilli(),
	}
}

func parseToolArguments(raw string) map[string]any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(trimmed), &out); err == nil {
		return out
	}

	var anyValue any
	if err := json.Unmarshal([]byte(trimmed), &anyValue); err == nil {
		return map[string]any{"value": anyValue}
	}

	return map[string]any{"_raw": trimmed}
}

func parseOpenAINonStreamingResponse(resp *http.Response, requestModel model.Model) (stream.EventStream, error) {
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var out openAIChatResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, errors.New("openai response has no choices")
	}

	choice := out.Choices[0]
	assistantContent := []any{}

	text := extractOpenAIMessageText(choice.Message.Content)
	if strings.TrimSpace(text) != "" {
		assistantContent = append(assistantContent, model.TextContent{
			Type: model.ContentText,
			Text: text,
		})
	}

	toolCalls := make([]model.ToolCallContent, 0, len(choice.Message.ToolCalls))
	for i, tc := range choice.Message.ToolCalls {
		id := strings.TrimSpace(tc.ID)
		if id == "" {
			id = fmt.Sprintf("call_%d", i+1)
		}
		name := strings.TrimSpace(tc.Function.Name)
		if name == "" {
			name = "tool"
		}
		call := model.ToolCallContent{
			Type:      model.ContentToolCall,
			ID:        id,
			Name:      name,
			Arguments: parseToolArguments(tc.Function.Arguments),
		}
		assistantContent = append(assistantContent, call)
		toolCalls = append(toolCalls, call)
	}

	modelID := out.Model
	if modelID == "" {
		modelID = requestModel.ID
	}

	assistant := &model.AssistantMessage{
		Role:       model.RoleAssistant,
		ContentRaw: assistantContent,
		Provider:   "openai",
		Model:      modelID,
		StopReason: mapStopReason(choice.FinishReason),
		Usage: model.Usage{
			Input:  out.Usage.PromptTokens,
			Output: out.Usage.CompletionTokens,
			Total:  out.Usage.TotalTokens,
		},
		Timestamp: time.Now().UnixMilli(),
	}

	events := []stream.Event{{Type: stream.EventStart}}
	if text != "" {
		events = append(events, stream.Event{
			Type:  stream.EventTextDelta,
			Delta: text,
		})
	}
	for _, call := range toolCalls {
		events = append(events, stream.Event{
			Type:       stream.EventToolCall,
			ToolName:   call.Name,
			ToolCallID: call.ID,
			Arguments:  call.Arguments,
		})
	}
	events = append(events, stream.Event{
		Type:   stream.EventDone,
		Reason: assistant.StopReason,
	})

	return &stream.StaticEventStream{
		Events:    events,
		ResultMsg: assistant,
	}, nil
}

func extractOpenAIMessageText(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	case []any:
		parts := []string{}
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				t, _ := m["type"].(string)
				if t == "text" {
					text, _ := m["text"].(string)
					if strings.TrimSpace(text) != "" {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func mapStopReason(reason string) model.StopReason {
	switch reason {
	case "length":
		return model.StopReasonLength
	case "tool_calls", "function_call":
		return model.StopReasonToolUse
	case "content_filter":
		return model.StopReasonError
	default:
		return model.StopReasonStop
	}
}
