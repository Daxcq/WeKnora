package chat

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/types"
)

type externalTestChat struct{}

func (externalTestChat) Chat(context.Context, []Message, *ChatOptions) (*types.ChatResponse, error) {
	return &types.ChatResponse{Content: "external"}, nil
}
func (externalTestChat) ChatStream(context.Context, []Message, *ChatOptions) (<-chan types.StreamResponse, error) {
	return make(chan types.StreamResponse), nil
}
func (externalTestChat) GetModelName() string { return "test" }
func (externalTestChat) GetModelID() string   { return "test" }

func TestNewRemoteChatUsesExternalFactory(t *testing.T) {
	name := provider.ProviderName("test-external-chat")
	if err := RegisterExternalChat(name, func(*ChatConfig) (Chat, error) { return externalTestChat{}, nil }); err != nil {
		t.Fatal(err)
	}
	defer UnregisterExternalChat(name)

	model, err := NewRemoteChat(&ChatConfig{Provider: string(name)})
	if err != nil {
		t.Fatal(err)
	}
	response, err := model.Chat(t.Context(), nil, nil)
	if err != nil || response.Content != "external" {
		t.Fatalf("unexpected external response: %#v, %v", response, err)
	}
}
