package sdk

import (
	"context"
	"strings"

	"github.com/zahlmann/phi/agent"
	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/provider"
	"github.com/zahlmann/phi/coding/session"
)

type PromptOptions struct {
	Images            []model.ImageContent
	StreamingBehavior string
}

type CreateSessionOptions struct {
	SystemPrompt   string
	Model          *model.Model
	ThinkingLevel  agent.ThinkingLevel
	Tools          []agent.Tool
	SessionManager session.Manager
	ProviderClient provider.Client
	AuthMode       provider.AuthMode
	APIKey         string
	AccessToken    string
	AccountID      string
}

type AgentSession struct {
	agent          *agent.Agent
	manager        session.Manager
	providerClient provider.Client
	authMode       provider.AuthMode
	apiKey         string
	accessToken    string
	accountID      string
}

func CreateAgentSession(options CreateSessionOptions) *AgentSession {
	manager := options.SessionManager
	if manager == nil {
		manager = session.NewInMemoryManager("session")
	}
	initial := agent.State{
		SystemPrompt: options.SystemPrompt,
		Model:        options.Model,
		Thinking:     options.ThinkingLevel,
		Messages:     []any{},
		Tools:        options.Tools,
	}
	return &AgentSession{
		agent:          agent.New(initial),
		manager:        manager,
		providerClient: options.ProviderClient,
		authMode:       options.AuthMode,
		apiKey:         options.APIKey,
		accessToken:    options.AccessToken,
		accountID:      options.AccountID,
	}
}

func (s *AgentSession) Prompt(text string, options PromptOptions) error {
	msg := userMessage(text, options.Images)
	if s.agent.State().IsStreaming {
		switch options.StreamingBehavior {
		case "followUp":
			s.agent.FollowUp(msg)
			return nil
		case "steer":
			s.agent.Steer(msg)
			return nil
		}
	}

	s.agent.Prompt(msg)
	if _, err := s.manager.AppendMessage(msg); err != nil {
		return err
	}

	if s.providerClient == nil {
		return nil
	}

	beforeCount := len(s.agent.State().Messages)
	if _, err := s.agent.RunTurn(context.Background(), agent.RunnerOptions{
		Client:      s.providerClient,
		AuthMode:    s.authMode,
		APIKey:      s.apiKey,
		AccessToken: s.accessToken,
		AccountID:   s.accountID,
		SessionID:   s.manager.SessionID(),
	}); err != nil {
		return err
	}

	after := s.agent.State().Messages
	for i := beforeCount; i < len(after); i++ {
		if _, err := s.manager.AppendMessage(after[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *AgentSession) Steer(text string) {
	s.agent.Steer(userMessage(text, nil))
}

func (s *AgentSession) FollowUp(text string) {
	s.agent.FollowUp(userMessage(text, nil))
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
