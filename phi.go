package phi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/zahlmann/phi/agent"
	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/provider"
	bashtool "github.com/zahlmann/phi/coding/bash"
	"github.com/zahlmann/phi/coding/sdk"
)

const defaultQueueCapacity = 256

type EventType string

const (
	EventToolCallStarted  EventType = "tool_call_started"
	EventToolCallFinished EventType = "tool_call_finished"
	EventFinalMessage     EventType = "final_message"
)

type Event struct {
	Type             EventType               `json:"type"`
	SessionID        string                  `json:"sessionId"`
	ToolName         string                  `json:"toolName,omitempty"`
	ToolCallID       string                  `json:"toolCallId,omitempty"`
	IsError          bool                    `json:"isError,omitempty"`
	ToolResult       *model.Message          `json:"toolResult,omitempty"`
	AssistantMessage *model.AssistantMessage `json:"assistantMessage,omitempty"`
	Error            string                  `json:"error,omitempty"`
}

type RuntimeOptions struct {
	ProviderClient provider.Client
	AuthMode       provider.AuthMode
	APIKey         string
	AccessToken    string
	AccountID      string
	ModelID        string
	SystemPrompt   string
	WorkingDir     string
	MaxToolRounds  int
	QueueCapacity  int
}

type StartSessionRequest struct {
	Prompt string
	Images []model.ImageContent
}

type StartSessionResponse struct {
	SessionID string
}

type QueueMessageRequest struct {
	SessionID string
	Prompt    string
	Images    []model.ImageContent
}

type Runtime struct {
	modelID       string
	systemPrompt  string
	workingDir    string
	authMode      provider.AuthMode
	apiKey        string
	accessToken   string
	accountID     string
	client        provider.Client
	maxToolRounds int
	queueCap      int

	mu       sync.RWMutex
	sessions map[string]*sessionRuntime

	eventMu     sync.RWMutex
	subscribers []func(Event)
}

type sessionRuntime struct {
	id          string
	session     *sdk.AgentSession
	unsubscribe func()
	runtime     *Runtime

	mu         sync.Mutex
	processing bool
}

func NewRuntime(options RuntimeOptions) *Runtime {
	authMode := options.AuthMode
	if strings.TrimSpace(string(authMode)) == "" {
		authMode = provider.AuthModeOpenAIAPIKey
	}

	modelID := strings.TrimSpace(options.ModelID)
	if modelID == "" {
		modelID = "gpt-5.2-codex"
		if authMode == provider.AuthModeChatGPT {
			modelID = "gpt-5.3-codex"
		}
	}

	client := options.ProviderClient
	if client == nil {
		client = provider.NewOpenAIClient()
	}

	queueCap := options.QueueCapacity
	if queueCap <= 0 {
		queueCap = defaultQueueCapacity
	}

	return &Runtime{
		modelID:       modelID,
		systemPrompt:  options.SystemPrompt,
		workingDir:    options.WorkingDir,
		authMode:      authMode,
		apiKey:        options.APIKey,
		accessToken:   options.AccessToken,
		accountID:     options.AccountID,
		client:        client,
		maxToolRounds: options.MaxToolRounds,
		queueCap:      queueCap,
		sessions:      map[string]*sessionRuntime{},
	}
}

func (r *Runtime) Subscribe(handler func(Event)) (unsubscribe func()) {
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	r.subscribers = append(r.subscribers, handler)
	idx := len(r.subscribers) - 1
	return func() {
		r.eventMu.Lock()
		defer r.eventMu.Unlock()
		if idx >= 0 && idx < len(r.subscribers) {
			r.subscribers[idx] = nil
		}
	}
}

func (r *Runtime) StartSession(ctx context.Context, request StartSessionRequest) (StartSessionResponse, error) {
	if err := validateUserInput(request.Prompt, request.Images); err != nil {
		return StartSessionResponse{}, err
	}
	select {
	case <-ctx.Done():
		return StartSessionResponse{}, ctx.Err()
	default:
	}

	sessionID := newSessionID()
	sessionHandle := r.newSession(sessionID)
	if err := r.storeSession(sessionID, sessionHandle); err != nil {
		return StartSessionResponse{}, err
	}
	if err := sessionHandle.enqueue(buildUserMessage(request.Prompt, request.Images)); err != nil {
		return StartSessionResponse{}, err
	}
	return StartSessionResponse{SessionID: sessionID}, nil
}

func (r *Runtime) QueueMessage(ctx context.Context, request QueueMessageRequest) error {
	if strings.TrimSpace(request.SessionID) == "" {
		return errors.New("session id is required")
	}
	if err := validateUserInput(request.Prompt, request.Images); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	sessionHandle, ok := r.getSession(request.SessionID)
	if !ok {
		return errors.New("session not found")
	}
	return sessionHandle.enqueue(buildUserMessage(request.Prompt, request.Images))
}

func (r *Runtime) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, handle := range r.sessions {
		if handle != nil && handle.unsubscribe != nil {
			handle.unsubscribe()
		}
	}
	r.sessions = map[string]*sessionRuntime{}
}

func (r *Runtime) newSession(sessionID string) *sessionRuntime {
	agentSession := sdk.CreateAgentSession(sdk.CreateSessionOptions{
		SystemPrompt:   r.systemPrompt,
		Model:          &model.Model{Provider: "openai", ID: r.modelID},
		ThinkingLevel:  agent.ThinkingXHigh,
		Tools:          []agent.Tool{bashtool.NewTool(r.workingDir, 0)},
		MaxToolRounds:  r.maxToolRounds,
		ProviderClient: r.client,
		AuthMode:       r.authMode,
		APIKey:         r.apiKey,
		AccessToken:    r.accessToken,
		AccountID:      r.accountID,
	})
	handle := &sessionRuntime{
		id:      sessionID,
		session: agentSession,
		runtime: r,
	}
	handle.unsubscribe = agentSession.Subscribe(func(event agent.Event) {
		r.forwardAgentEvent(sessionID, event)
	})
	return handle
}

func (r *Runtime) storeSession(sessionID string, handle *sessionRuntime) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sessions[sessionID]; exists {
		return errors.New("session id collision")
	}
	r.sessions[sessionID] = handle
	return nil
}

func (r *Runtime) getSession(sessionID string) (*sessionRuntime, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	handle, ok := r.sessions[sessionID]
	return handle, ok
}

func (r *Runtime) emit(event Event) {
	r.eventMu.RLock()
	handlers := append([]func(Event){}, r.subscribers...)
	r.eventMu.RUnlock()
	for _, handler := range handlers {
		if handler != nil {
			handler(event)
		}
	}
}

func (r *Runtime) forwardAgentEvent(sessionID string, event agent.Event) {
	switch event.Type {
	case agent.EventToolExecutionStart:
		r.emit(Event{
			Type:       EventToolCallStarted,
			SessionID:  sessionID,
			ToolName:   event.ToolName,
			ToolCallID: event.ToolCallID,
		})
	case agent.EventToolExecutionEnd:
		out := Event{
			Type:       EventToolCallFinished,
			SessionID:  sessionID,
			ToolName:   event.ToolName,
			ToolCallID: event.ToolCallID,
			IsError:    event.IsError,
		}
		if toolResult, ok := event.Message.(model.Message); ok {
			toolCopy := toolResult
			out.ToolResult = &toolCopy
		}
		r.emit(out)
	}
}

func (s *sessionRuntime) enqueue(message model.Message) error {
	if s.session.PendingCount() >= s.runtime.queueCap {
		return errors.New("session queue is full")
	}
	s.session.Queue(message)
	s.triggerDrain()
	return nil
}

func (s *sessionRuntime) triggerDrain() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.processing {
		return
	}
	s.processing = true
	go s.drain()
}

func (s *sessionRuntime) drain() {
	for {
		queued, ok := s.nextQueuedMessage()
		if !ok {
			return
		}

		assistant, err := s.session.ProcessQueuedMessage(context.Background(), queued)
		if err != nil {
			failed := assistantErrorMessage(s.session.State(), err)
			_ = s.session.AppendAssistantMessage(failed)
			s.runtime.emit(Event{
				Type:             EventFinalMessage,
				SessionID:        s.id,
				AssistantMessage: &failed,
				Error:            err.Error(),
			})
			continue
		}
		if assistant != nil {
			assistantCopy := *assistant
			s.runtime.emit(Event{
				Type:             EventFinalMessage,
				SessionID:        s.id,
				AssistantMessage: &assistantCopy,
			})
		}
	}
}

func (s *sessionRuntime) nextQueuedMessage() (model.Message, bool) {
	queued, ok := s.session.PopQueuedMessage()
	if ok {
		return queued, true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	queued, ok = s.session.PopQueuedMessage()
	if !ok {
		s.processing = false
		return model.Message{}, false
	}
	return queued, true
}

func assistantErrorMessage(state agent.State, err error) model.AssistantMessage {
	providerName := "openai"
	modelID := ""
	if state.Model != nil {
		if strings.TrimSpace(state.Model.Provider) != "" {
			providerName = state.Model.Provider
		}
		modelID = state.Model.ID
	}
	return model.AssistantMessage{
		Role: model.RoleAssistant,
		ContentRaw: []any{
			model.TextContent{
				Type: model.ContentText,
				Text: "Assistant turn failed: " + err.Error(),
			},
		},
		Provider:     providerName,
		Model:        modelID,
		StopReason:   model.StopReasonError,
		ErrorMessage: err.Error(),
		Timestamp:    time.Now().UnixMilli(),
	}
}

func validateUserInput(prompt string, images []model.ImageContent) error {
	if strings.TrimSpace(prompt) == "" && len(images) == 0 {
		return errors.New("prompt or images are required")
	}
	return nil
}

func buildUserMessage(prompt string, images []model.ImageContent) model.Message {
	content := make([]any, 0, 1+len(images))
	if strings.TrimSpace(prompt) != "" {
		content = append(content, model.TextContent{
			Type: model.ContentText,
			Text: prompt,
		})
	}
	for _, image := range images {
		content = append(content, image)
	}
	return model.Message{
		Role:       model.RoleUser,
		ContentRaw: content,
	}
}

func newSessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		now := time.Now().UTC().UnixNano()
		return "session-" + strings.ReplaceAll(time.Unix(0, now).UTC().Format("20060102T150405.000000000"), ".", "")
	}
	return "session-" + hex.EncodeToString(buf)
}
