package agent

import "github.com/zahlmann/phi/ai/model"

type ThinkingLevel string

const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
)

type EventType string

const (
	EventAgentStart         EventType = "agent_start"
	EventAgentEnd           EventType = "agent_end"
	EventTurnStart          EventType = "turn_start"
	EventTurnEnd            EventType = "turn_end"
	EventMessageStart       EventType = "message_start"
	EventMessageUpdate      EventType = "message_update"
	EventMessageEnd         EventType = "message_end"
	EventToolExecutionStart EventType = "tool_execution_start"
	EventToolExecutionEnd   EventType = "tool_execution_end"
)

type Event struct {
	Type       EventType `json:"type"`
	Message    any       `json:"message,omitempty"`
	ToolName   string    `json:"toolName,omitempty"`
	ToolCallID string    `json:"toolCallId,omitempty"`
	IsError    bool      `json:"isError,omitempty"`
}

type ToolResult struct {
	Content []model.TextContent `json:"content"`
	Details map[string]any      `json:"details,omitempty"`
}

type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(toolCallID string, args map[string]any) (ToolResult, error)
}

type State struct {
	SystemPrompt string        `json:"systemPrompt"`
	Model        *model.Model  `json:"model,omitempty"`
	Thinking     ThinkingLevel `json:"thinkingLevel"`
	Messages     []any         `json:"messages"`
	IsStreaming  bool          `json:"isStreaming"`
	Tools        []Tool        `json:"-"`
}
