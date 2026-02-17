package model

type Role string

const (
	RoleUser       Role = "user"
	RoleAssistant  Role = "assistant"
	RoleToolResult Role = "toolResult"
)

type ContentType string

const (
	ContentText     ContentType = "text"
	ContentToolCall ContentType = "toolCall"
	ContentImage    ContentType = "image"
	ContentThinking ContentType = "thinking"
)

type TextContent struct {
	Type ContentType `json:"type"`
	Text string      `json:"text"`
}

type ImageContent struct {
	Type     ContentType `json:"type"`
	MIMEType string      `json:"mimeType"`
	Data     string      `json:"data"`
}

type ToolCallContent struct {
	Type      ContentType    `json:"type"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ThinkingContent struct {
	Type     ContentType `json:"type"`
	Thinking string      `json:"thinking"`
}

type Message struct {
	Role       Role   `json:"role"`
	ContentRaw []any  `json:"content"`
	ToolCallID string `json:"toolCallId,omitempty"`
	ToolName   string `json:"toolName,omitempty"`
	Timestamp  int64  `json:"timestamp,omitempty"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Context struct {
	SystemPrompt string    `json:"systemPrompt,omitempty"`
	Messages     []Message `json:"messages"`
	Tools        []Tool    `json:"tools,omitempty"`
}

type Model struct {
	Provider      string `json:"provider"`
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	MaxTokens     int    `json:"maxTokens,omitempty"`
	Reasoning     bool   `json:"reasoning"`
}

type Usage struct {
	Input  int     `json:"input"`
	Output int     `json:"output"`
	Total  int     `json:"total"`
	Cost   float64 `json:"cost"`
}

type StopReason string

const (
	StopReasonStop    StopReason = "stop"
	StopReasonLength  StopReason = "length"
	StopReasonToolUse StopReason = "toolUse"
	StopReasonError   StopReason = "error"
	StopReasonAborted StopReason = "aborted"
)

type AssistantMessage struct {
	Role         Role       `json:"role"`
	ContentRaw   []any      `json:"content"`
	Provider     string     `json:"provider"`
	Model        string     `json:"model"`
	StopReason   StopReason `json:"stopReason"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
	Usage        Usage      `json:"usage"`
	Timestamp    int64      `json:"timestamp"`
}
