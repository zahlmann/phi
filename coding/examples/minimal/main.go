package main

import (
	"context"
	"fmt"
	"time"

	"github.com/zahlmann/phi"
)

func main() {
	rt := phi.NewRuntime(phi.RuntimeOptions{
		SystemPrompt: "You are a concise coding assistant.",
		WorkingDir:   ".",
	})
	defer rt.Close()

	final := make(chan phi.Event, 1)
	unsubscribe := rt.Subscribe(func(event phi.Event) {
		if event.Type == phi.EventFinalMessage {
			final <- event
		}
	})
	defer unsubscribe()

	_, err := rt.StartSession(context.Background(), phi.StartSessionRequest{
		Prompt: "Use bash to run: pwd",
	})
	if err != nil {
		panic(err)
	}

	select {
	case event := <-final:
		if event.AssistantMessage != nil {
			fmt.Printf("[assistant_final] %v\n", event.AssistantMessage.ContentRaw)
		}
		if event.Error != "" {
			fmt.Printf("[assistant_error] %s\n", event.Error)
		}
	case <-time.After(60 * time.Second):
		panic("timed out waiting for final message")
	}
}
