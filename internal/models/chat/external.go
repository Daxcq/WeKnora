package chat

import (
	"fmt"
	"sync"

	"github.com/Tencent/WeKnora/internal/models/provider"
)

type ExternalChatFactory func(*ChatConfig) (Chat, error)

var externalChats = struct {
	sync.RWMutex
	factories map[provider.ProviderName]ExternalChatFactory
}{factories: make(map[provider.ProviderName]ExternalChatFactory)}

func RegisterExternalChat(name provider.ProviderName, factory ExternalChatFactory) error {
	if name == "" || factory == nil {
		return fmt.Errorf("external chat provider requires name and factory")
	}
	externalChats.Lock()
	defer externalChats.Unlock()
	if _, exists := externalChats.factories[name]; exists {
		return fmt.Errorf("external chat provider %s already registered", name)
	}
	externalChats.factories[name] = factory
	return nil
}

func UnregisterExternalChat(name provider.ProviderName) {
	externalChats.Lock()
	defer externalChats.Unlock()
	delete(externalChats.factories, name)
}

func externalChat(name provider.ProviderName, config *ChatConfig) (Chat, bool, error) {
	externalChats.RLock()
	factory, ok := externalChats.factories[name]
	externalChats.RUnlock()
	if !ok {
		return nil, false, nil
	}
	model, err := factory(config)
	return model, true, err
}
