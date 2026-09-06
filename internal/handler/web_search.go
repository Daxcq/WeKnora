package handler

import (
	"net/http"

	infra_web_search "github.com/Tencent/WeKnora/internal/infrastructure/web_search"
	"github.com/gin-gonic/gin"
)

// WebSearchHandler handles legacy web search related requests
type WebSearchHandler struct{ registry *infra_web_search.Registry }

// NewWebSearchHandler creates a new web search handler
func NewWebSearchHandler(registry *infra_web_search.Registry) *WebSearchHandler {
	return &WebSearchHandler{registry: registry}
}

// GetProviders returns the list of available web search provider types.
//
// GetProviders godoc
// @Summary      获取可用网络搜索 Provider 列表
// @Description  返回所有已注册的网络搜索 provider（含元数据）
// @Tags         网络搜索
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "provider 列表"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /web-search/providers [get]
func (h *WebSearchHandler) GetProviders(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    h.registry.ProviderTypes(),
	})
}
