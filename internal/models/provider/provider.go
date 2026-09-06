// Package provider defines the unified interface and registry for multi-vendor model API adapters.
package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
)

// ProviderName 模型服务商名称
type ProviderName string

const (
	// OpenAI
	ProviderOpenAI ProviderName = "openai"
	// Anthropic Claude
	ProviderAnthropic ProviderName = "anthropic"
	// 阿里云 DashScope
	ProviderAliyun ProviderName = "aliyun"
	// 智谱AI (GLM 系列)
	ProviderZhipu ProviderName = "zhipu"
	// OpenRouter
	ProviderOpenRouter ProviderName = "openrouter"
	// 硅基流动
	ProviderSiliconFlow ProviderName = "siliconflow"
	// Jina AI (Embedding and Rerank)
	ProviderJina ProviderName = "jina"
	// Generic 兼容OpenAI (自定义部署)
	ProviderGeneric ProviderName = "generic"
	// DeepSeek
	ProviderDeepSeek ProviderName = "deepseek"
	// Google Gemini
	ProviderGemini ProviderName = "gemini"
	// 火山引擎 Ark
	ProviderVolcengine ProviderName = "volcengine"
	// 腾讯混元
	ProviderHunyuan ProviderName = "hunyuan"
	// MiniMax
	ProviderMiniMax ProviderName = "minimax"
	// 小米 Mimo
	ProviderMimo ProviderName = "mimo"
	// GPUStack (私有化部署)
	ProviderGPUStack ProviderName = "gpustack"
	// 月之暗面 Moonshot (Kimi)
	ProviderMoonshot ProviderName = "moonshot"
	// 魔搭 ModelScope
	ProviderModelScope ProviderName = "modelscope"
	// 百度千帆
	ProviderQianfan ProviderName = "qianfan"
	// 七牛云
	ProviderQiniu ProviderName = "qiniu"
	// 美团 LongCat AI
	ProviderLongCat ProviderName = "longcat"
	// 腾讯云 LKEAP (知识引擎原子能力)
	ProviderLKEAP ProviderName = "lkeap"
	// NVIDIA
	ProviderNvidia ProviderName = "nvidia"
	// Novita AI
	ProviderNovita ProviderName = "novita"
	// Azure OpenAI
	ProviderAzureOpenAI ProviderName = "azure_openai"
)

// AllProviders 返回所有注册的提供者名称
func AllProviders() []ProviderName {
	return []ProviderName{
		ProviderGeneric,
		ProviderWeKnoraCloud,
		ProviderAliyun,
		ProviderZhipu,
		ProviderVolcengine,
		ProviderHunyuan,
		ProviderSiliconFlow,
		ProviderDeepSeek,
		ProviderMiniMax,
		ProviderMoonshot,
		ProviderModelScope,
		ProviderQianfan,
		ProviderQiniu,
		ProviderOpenAI,
		ProviderAnthropic,
		ProviderGemini,
		ProviderOpenRouter,
		ProviderJina,
		ProviderMimo,
		ProviderLongCat,
		ProviderLKEAP,
		ProviderGPUStack,
		ProviderNvidia,
		ProviderNovita,
		ProviderAzureOpenAI,
	}
}

// ProviderInfo 包含提供者的元数据
type ProviderInfo struct {
	Name         ProviderName               // 提供者标识
	DisplayName  string                     // 可读名称
	Description  string                     // 提供者描述
	DefaultURLs  map[types.ModelType]string // 按模型类型区分的默认 BaseURL
	ModelTypes   []types.ModelType          // 支持的模型类型
	RequiresAuth bool                       // 是否需要 API key
	ExtraFields  []ExtraFieldConfig         // 额外配置字段
	External     bool                       `json:"external,omitempty"`
	Permissions  *ProviderPermissions       `json:"permissions,omitempty"`
}

type ProviderPermissions struct {
	AllowNetwork bool     `json:"allow_network"`
	ReadPaths    []string `json:"read_paths,omitempty"`
}

type ProviderStatus struct {
	Type    string `json:"type"`
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
}

// GetDefaultURL 获取指定模型类型的默认 URL
func (p ProviderInfo) GetDefaultURL(modelType types.ModelType) string {
	if url, ok := p.DefaultURLs[modelType]; ok {
		return url
	}
	// 回退到 Chat URL
	if url, ok := p.DefaultURLs[types.ModelTypeKnowledgeQA]; ok {
		return url
	}
	return ""
}

// ExtraFieldConfig 定义提供者的额外配置字段
type ExtraFieldConfig struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"` // "string", "number", "boolean", "select"
	Required    bool   `json:"required"`
	Default     string `json:"default"`
	Placeholder string `json:"placeholder"`
	Options     []struct {
		Label string `json:"label"`
		Value string `json:"value"`
	} `json:"options,omitempty"`
}

// Config 表示模型提供者的配置
type Config struct {
	Provider  ProviderName   `json:"provider"`
	BaseURL   string         `json:"base_url"`
	APIKey    string         `json:"api_key"`
	ModelName string         `json:"model_name"`
	ModelID   string         `json:"model_id"`
	Extra     map[string]any `json:"extra,omitempty"`
}

type Provider interface {
	// Info 返回服务商的元数据
	Info() ProviderInfo

	// ValidateConfig 验证服务商的配置
	ValidateConfig(config *Config) error
}

// registry 存储所有注册的提供者
var (
	registryMu sync.RWMutex
	registry   = make(map[ProviderName]Provider)
)

// Register 添加一个提供者到全局注册表
func Register(p Provider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[p.Info().Name] = p
}

func RegisterExternal(info ProviderInfo, validate func(*Config) error, health func(context.Context) error) error {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[info.Name]; exists {
		return fmt.Errorf("model provider %s already registered", info.Name)
	}
	info.External = true
	registry[info.Name] = externalProvider{info: info, validate: validate, health: health}
	return nil
}

func UnregisterExternal(name ProviderName) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if p, ok := registry[name]; ok && p.Info().External {
		delete(registry, name)
	}
}

type externalProvider struct {
	info     ProviderInfo
	validate func(*Config) error
	health   func(context.Context) error
}

func ExternalStatuses(ctx context.Context) []ProviderStatus {
	registryMu.RLock()
	external := make([]externalProvider, 0)
	for _, p := range registry {
		if ep, ok := p.(externalProvider); ok {
			external = append(external, ep)
		}
	}
	registryMu.RUnlock()
	statuses := make([]ProviderStatus, 0, len(external))
	for _, p := range external {
		status := ProviderStatus{Type: string(p.info.Name), Healthy: true}
		if p.health != nil {
			if err := p.health(ctx); err != nil {
				status.Healthy = false
				status.Error = err.Error()
			}
		}
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Type < statuses[j].Type })
	return statuses
}

func (p externalProvider) Info() ProviderInfo { return p.info }
func (p externalProvider) ValidateConfig(config *Config) error {
	if p.validate == nil {
		return nil
	}
	return p.validate(config)
}

// Get 通过名称从注册表中获取提供者
func Get(name ProviderName) (Provider, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	p, ok := registry[name]
	return p, ok
}

// GetOrDefault 通过名称从注册表中获取提供者，如果未找到则返回默认提供者
func GetOrDefault(name ProviderName) Provider {
	p, ok := Get(name)
	if ok {
		return p
	}
	// 如果未找到则返回默认提供者
	p, _ = Get(ProviderGeneric)
	return p
}

// List 返回所有注册的提供者（按 AllProviders 定义的顺序）
func List() []ProviderInfo {
	registryMu.RLock()
	defer registryMu.RUnlock()

	result := make([]ProviderInfo, 0, len(registry))
	seen := make(map[ProviderName]struct{}, len(registry))
	for _, name := range AllProviders() {
		if p, ok := registry[name]; ok {
			result = append(result, p.Info())
			seen[name] = struct{}{}
		}
	}
	external := make([]ProviderInfo, 0)
	for name, p := range registry {
		if _, ok := seen[name]; !ok {
			external = append(external, p.Info())
		}
	}
	sort.Slice(external, func(i, j int) bool { return external[i].Name < external[j].Name })
	result = append(result, external...)
	return result
}

// ListByModelType 返回所有支持指定模型类型的提供者（按 AllProviders 定义的顺序）
func ListByModelType(modelType types.ModelType) []ProviderInfo {
	registryMu.RLock()
	defer registryMu.RUnlock()

	result := make([]ProviderInfo, 0)
	seen := make(map[ProviderName]struct{}, len(registry))
	for _, name := range AllProviders() {
		if p, ok := registry[name]; ok {
			seen[name] = struct{}{}
			info := p.Info()
			for _, t := range info.ModelTypes {
				if t == modelType {
					result = append(result, info)
					break
				}
			}
		}
	}
	external := make([]ProviderInfo, 0)
	for name, p := range registry {
		if _, builtin := seen[name]; builtin {
			continue
		}
		info := p.Info()
		for _, t := range info.ModelTypes {
			if t == modelType {
				external = append(external, info)
				break
			}
		}
	}
	sort.Slice(external, func(i, j int) bool { return external[i].Name < external[j].Name })
	result = append(result, external...)
	return result
}

// DetectProvider 通过 BaseURL 检测服务商
func DetectProvider(baseURL string) ProviderName {
	switch {
	case containsAny(baseURL, "dashscope.aliyuncs.com"):
		return ProviderAliyun
	case containsAny(baseURL, "open.bigmodel.cn", "zhipu"):
		return ProviderZhipu
	case containsAny(baseURL, "openrouter.ai"):
		return ProviderOpenRouter
	case containsAny(baseURL, "siliconflow.cn"):
		return ProviderSiliconFlow
	case containsAny(baseURL, "api.jina.ai"):
		return ProviderJina
	case containsAny(baseURL, "openai.azure.com"):
		return ProviderAzureOpenAI
	case containsAny(baseURL, "api.openai.com"):
		return ProviderOpenAI
	case containsAny(baseURL, "api.anthropic.com"):
		return ProviderAnthropic
	case containsAny(baseURL, "api.deepseek.com"):
		return ProviderDeepSeek
	case containsAny(baseURL, "generativelanguage.googleapis.com"):
		return ProviderGemini
	case containsAny(baseURL, "volces.com", "volcengine"):
		return ProviderVolcengine
	case containsAny(baseURL, "hunyuan.cloud.tencent.com"):
		return ProviderHunyuan
	case containsAny(baseURL, "minimax.io", "minimaxi.com"):
		return ProviderMiniMax
	case containsAny(baseURL, "xiaomimimo.com"):
		return ProviderMimo
	case containsAny(baseURL, "gpustack"):
		return ProviderGPUStack
	case containsAny(baseURL, "modelscope.cn"):
		return ProviderModelScope
	case containsAny(baseURL, "qiniuapi.com", "qiniu"):
		return ProviderQiniu
	case containsAny(baseURL, "moonshot.ai"):
		return ProviderMoonshot
	case containsAny(baseURL, "qianfan.baidubce.com", "baidubce.com"):
		return ProviderQianfan
	case containsAny(baseURL, "longcat.chat"):
		return ProviderLongCat
	case containsAny(baseURL, "lkeap.cloud.tencent.com", "api.lkeap", "lkeap.tencentcloudapi.com"):
		return ProviderLKEAP
	case containsAny(baseURL, "nvidia.com"):
		return ProviderNvidia
	case containsAny(baseURL, "api.novita.ai", "novita.ai"):
		return ProviderNovita
	case containsAny(baseURL, "weknora.weixin.qq.com"):
		return ProviderWeKnoraCloud
	default:
		return ProviderGeneric
	}
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func NewConfigFromModel(model *types.Model) (*Config, error) {
	if model == nil {
		return nil, fmt.Errorf("model is nil")
	}

	providerName := ProviderName(model.Parameters.Provider)
	if providerName == "" {
		providerName = DetectProvider(model.Parameters.BaseURL)
	}

	extra := make(map[string]any, len(model.Parameters.ExtraConfig))
	for key, value := range model.Parameters.ExtraConfig {
		extra[key] = value
	}
	return &Config{
		Provider:  providerName,
		BaseURL:   model.Parameters.BaseURL,
		APIKey:    model.Parameters.APIKey,
		ModelName: model.Name,
		ModelID:   model.ID,
		Extra:     extra,
	}, nil
}
