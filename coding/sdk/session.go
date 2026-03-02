package sdk

import (
	"context"
	"strings"

	"github.com/zahlmann/phi/agent"
	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/provider"
)

type CreateSessionOptions struct {
	SystemPrompt   string
	Model          *model.Model
	ThinkingLevel  agent.ThinkingLevel
	Tools          []agent.Tool
	MaxToolRounds  int
	ProviderClient provider.Client
	AuthMode       provider.AuthMode
	APIKey         string
	AccessToken    string
	AccountID      string
}

type AgentSession struct {
	agent          *agent.Agent
	providerClient provider.Client
	authMode       provider.AuthMode
	apiKey         string
	accessToken    string
	accountID      string
	maxToolRounds  int
}

func CreateAgentSession(options CreateSessionOptions) *AgentSession {
	initial := agent.State{
		SystemPrompt: options.SystemPrompt,
		Model:        options.Model,
		Thinking:     options.ThinkingLevel,
		Messages:     []any{},
		Tools:        options.Tools,
	}
	return &AgentSession{
		agent:          agent.New(initial),
		providerClient: options.ProviderClient,
		authMode:       options.AuthMode,
		apiKey:         options.APIKey,
		accessToken:    options.AccessToken,
		accountID:      options.AccountID,
		maxToolRounds:  options.MaxToolRounds,
	}
}

func (s *AgentSession) Prompt(text string, images []model.ImageContent) error {
	if s.agent.State().IsStreaming {
		s.QueueMessage(text, images)
		return nil
	}
	msg := userMessage(text, images)
	_, err := s.processMessage(context.Background(), msg)
	return err
}

func (s *AgentSession) QueueMessage(text string, images []model.ImageContent) {
	s.agent.Queue(userMessage(text, images))
}

func (s *AgentSession) Queue(message model.Message) {
	s.agent.Queue(message)
}

func (s *AgentSession) PopQueuedMessage() (model.Message, bool) {
	return s.agent.DequeuePending()
}

func (s *AgentSession) PendingCount() int {
	return s.agent.PendingCount()
}

func (s *AgentSession) ProcessQueuedMessage(ctx context.Context, queued model.Message) (*model.AssistantMessage, error) {
	return s.processMessage(ctx, queued)
}

func (s *AgentSession) AppendAssistantMessage(message model.AssistantMessage) error {
	s.agent.AddMessage(message)
	return nil
}

func (s *AgentSession) processMessage(ctx context.Context, msg model.Message) (*model.AssistantMessage, error) {
	s.agent.Prompt(msg)

	if s.providerClient == nil {
		return nil, nil
	}

	assistant, err := s.agent.RunTurn(ctx, agent.RunnerOptions{
		Client:        s.providerClient,
		AuthMode:      s.authMode,
		APIKey:        s.apiKey,
		AccessToken:   s.accessToken,
		AccountID:     s.accountID,
		MaxToolRounds: s.maxToolRounds,
	})
	return assistant, err
}

func (s *AgentSession) Subscribe(handler func(agent.Event)) (unsubscribe func()) {
	return s.agent.Subscribe(handler)
}

func (s *AgentSession) State() agent.State {
	return s.agent.State()
}

func userMessage(text string, images []model.ImageContent) model.Message {
	content := make([]any, 0, 1+len(images))
	if strings.TrimSpace(text) != "" {
		content = append(content, model.TextContent{
			Type: model.ContentText,
			Text: text,
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
