package management

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// RefreshAuthFileCredential 强制刷新一个 OAuth 凭据，响应中不会暴露令牌。
func (h *Handler) RefreshAuthFileCredential(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code": "auth_manager_unavailable", "message": "core auth manager unavailable", "retryable": true,
		})
		return
	}
	var request struct {
		ID        string `json:"id"`
		AuthIndex string `json:"auth_index"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "invalid_request", "message": "invalid request body", "retryable": false,
		})
		return
	}
	request.ID = strings.TrimSpace(request.ID)
	request.AuthIndex = strings.TrimSpace(request.AuthIndex)
	result, err := h.authManager.ForceRefreshCredential(c.Request.Context(), request.ID, request.AuthIndex)
	if err != nil {
		h.writeCredentialRefreshError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":       "ok",
		"id":           result.ID,
		"auth_index":   result.AuthIndex,
		"provider":     result.Provider,
		"refreshed_at": result.RefreshedAt,
	})
}

func (h *Handler) writeCredentialRefreshError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, coreauth.ErrCredentialRefreshSelectorRequired), errors.Is(err, coreauth.ErrCredentialRefreshSelectorMismatch):
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "invalid_auth_selector", "message": "id or auth_index must identify exactly one auth", "retryable": false,
		})
	case errors.Is(err, coreauth.ErrCredentialRefreshAuthNotFound):
		c.JSON(http.StatusNotFound, gin.H{
			"code": "auth_not_found", "message": "auth was not found", "retryable": false,
		})
	case errors.Is(err, coreauth.ErrCredentialRefreshUnsupported):
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"code": "credential_refresh_unsupported", "message": "auth does not support a durable OAuth credential refresh", "retryable": false,
		})
	case errors.Is(err, coreauth.ErrCredentialRefreshPersistFailed):
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": "credential_persist_failed", "message": "refreshed credential was not activated because persistence failed", "retryable": true,
		})
	case errors.Is(err, coreauth.ErrCredentialReauthorizationRequired):
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"code": "reauthorization_required", "message": "refresh credential is no longer valid; reauthorization is required", "retryable": false,
		})
	default:
		c.JSON(http.StatusBadGateway, gin.H{
			"code": "credential_refresh_failed", "message": "OAuth credential refresh failed", "retryable": true,
		})
	}
}
