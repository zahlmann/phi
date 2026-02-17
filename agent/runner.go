package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/provider"
	"github.com/zahlmann/phi/ai/stream"
)

type RunnerOptions struct {
	Client        provider.Client
	APIKey        string
	SessionID     string
	Tools         []Tool
	MaxToolRounds int
}

func (a *Agent) RunTurn(ctx context.Context, options RunnerOptions) (*model.AssistantMessage, error) {
	if options.Client == nil {
		return nil, errors.New("provider client is required")
	}
	state := a.State()
	if state.Model == nil {
		return nil, errors.New("model is required")
	}

	tools := options.Tools
	if len(tools) == 0 {
		tools = state.Tools
	}
	maxRounds := options.MaxToolRounds
	if maxRounds <= 0 {
		maxRounds = 8
	}

	a.emit(Event{Type: EventTurnStart})
	a.setStreaming(true)
	defer a.setStreaming(false)

	var lastAssistant *model.AssistantMessage
	for round := 0; round < maxRounds; round++ {
		conversation := model.Context{
			SystemPrompt: state.SystemPrompt,
			Messages:     toModelMessages(a.State().Messages),
			Tools:        toModelTools(tools),
		}

		evStream, err := options.Client.Stream(ctx, *state.Model, conversation, provider.StreamOptions{
			APIKey:    options.APIKey,
			SessionID: options.SessionID,
		})
		if err != nil {
			return nil, err
		}

		for {
			ev, recvErr := evStream.Recv()
			if recvErr != nil {
				break
			}
			a.emit(Event{
				Type:    mapStreamEventType(ev.Type),
				Message: ev,
			})
		}

		result, err := evStream.Result()
		_ = evStream.Close()
		if err != nil {
			return nil, err
		}
		if result.Timestamp == 0 {
			result.Timestamp = time.Now().UnixMilli()
		}

		a.appendMessage(*result)
		a.emit(Event{Type: EventMessageEnd, Message: *result})
		lastAssistant = result

		toolCalls := extractToolCalls(result.ContentRaw)
		if len(toolCalls) == 0 || result.StopReason != model.StopReasonToolUse {
			a.emit(Event{Type: EventTurnEnd})
			return result, nil
		}

		for _, call := range toolCalls {
			toolResultMessage, hasError := executeToolCall(tools, call, a.emit)
			a.appendMessage(toolResultMessage)
			a.emit(Event{
				Type:       EventToolExecutionEnd,
				ToolName:   call.Name,
				ToolCallID: call.ID,
				IsError:    hasError,
				Message:    toolResultMessage,
			})
		}
	}

	a.emit(Event{Type: EventTurnEnd})
	if lastAssistant != nil {
		return lastAssistant, fmt.Errorf("max tool rounds reached without final assistant response")
	}
	return nil, fmt.Errorf("max tool rounds reached without assistant response")
}

func executeToolCall(tools []Tool, call model.ToolCallContent, emit func(Event)) (model.Message, bool) {
	emit(Event{
		Type:       EventToolExecutionStart,
		ToolName:   call.Name,
		ToolCallID: call.ID,
	})

	tool := findTool(tools, call.Name)
	if tool == nil {
		return model.Message{
			Role:       model.RoleToolResult,
			ToolCallID: call.ID,
			ToolName:   call.Name,
			ContentRaw: []any{
				model.TextContent{
					Type: model.ContentText,
					Text: "Tool not found: " + call.Name,
				},
			},
			Timestamp: time.Now().UnixMilli(),
		}, true
	}

	result, err := tool.Execute(call.ID, call.Arguments)
	if err != nil {
		return model.Message{
			Role:       model.RoleToolResult,
			ToolCallID: call.ID,
			ToolName:   call.Name,
			ContentRaw: []any{
				model.TextContent{
					Type: model.ContentText,
					Text: "Tool execution error: " + err.Error(),
				},
			},
			Timestamp: time.Now().UnixMilli(),
		}, true
	}

	content := make([]any, 0, len(result.Content))
	for _, item := range result.Content {
		content = append(content, item)
	}
	if len(content) == 0 {
		content = append(content, model.TextContent{
			Type: model.ContentText,
			Text: "(tool returned no output)",
		})
	}

	return model.Message{
		Role:       model.RoleToolResult,
		ToolCallID: call.ID,
		ToolName:   call.Name,
		ContentRaw: content,
		Timestamp:  time.Now().UnixMilli(),
	}, false
}

func findTool(tools []Tool, name string) Tool {
	for _, tool := range tools {
		if tool != nil && tool.Name() == name {
			return tool
		}
	}
	return nil
}

func extractToolCalls(content []any) []model.ToolCallContent {
	out := []model.ToolCallContent{}
	for _, item := range content {
		switch v := item.(type) {
		case model.ToolCallContent:
			out = append(out, v)
		case map[string]any:
			kind, _ := v["type"].(string)
			if kind != string(model.ContentToolCall) {
				continue
			}
			call := model.ToolCallContent{
				Type: model.ContentToolCall,
			}
			call.ID, _ = v["id"].(string)
			call.Name, _ = v["name"].(string)
			if args, ok := v["arguments"].(map[string]any); ok {
				call.Arguments = args
			} else {
				call.Arguments = map[string]any{}
			}
			out = append(out, call)
		}
	}
	return out
}

func toModelTools(tools []Tool) []model.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]model.Tool, 0, len(tools))
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		out = append(out, model.Tool{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
		})
	}
	return out
}

func (a *Agent) appendMessage(message any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Messages = append(a.state.Messages, message)
}

func (a *Agent) setStreaming(value bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.IsStreaming = value
}

func mapStreamEventType(t stream.EventType) EventType {
	switch t {
	case stream.EventStart:
		return EventMessageStart
	case stream.EventTextDelta, stream.EventThinkingDelta:
		return EventMessageUpdate
	case stream.EventToolCall:
		return EventToolExecutionStart
	case stream.EventDone:
		return EventMessageEnd
	case stream.EventError:
		return EventToolExecutionEnd
	default:
		return EventMessageUpdate
	}
}

func toModelMessages(in []any) []model.Message {
	out := make([]model.Message, 0, len(in))
	for _, item := range in {
		switch v := item.(type) {
		case model.Message:
			out = append(out, v)
		case model.AssistantMessage:
			out = append(out, model.Message{
				Role:       model.RoleAssistant,
				ContentRaw: v.ContentRaw,
				Timestamp:  v.Timestamp,
			})
		}
	}
	return out
}
