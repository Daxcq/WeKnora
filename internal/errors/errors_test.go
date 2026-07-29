package errors

import (
	stderrors "errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppError_Error(t *testing.T) {
	e := &AppError{Code: ErrBadRequest, Message: "bad input"}
	assert.Equal(t, "error code: 1000, error message: bad input", e.Error())
	// AppError must satisfy the standard error interface.
	var err error = e
	assert.Equal(t, e.Error(), err.Error())
}

func TestAppError_WithDetails(t *testing.T) {
	e := NewBadRequestError("oops")
	details := map[string]any{"field": "name"}

	returned := e.WithDetails(details)

	// WithDetails mutates in place and returns the same pointer for chaining.
	assert.Same(t, e, returned)
	assert.Equal(t, details, e.Details)
}

// TestConstructors covers every simple constructor: the code, the HTTP status,
// and (where the message is fixed) the message text.
func TestConstructors(t *testing.T) {
	tests := []struct {
		name     string
		err      *AppError
		wantCode ErrorCode
		wantHTTP int
		wantMsg  string
	}{
		{"bad request", NewBadRequestError("m"), ErrBadRequest, http.StatusBadRequest, "m"},
		{"unauthorized", NewUnauthorizedError("m"), ErrUnauthorized, http.StatusUnauthorized, "m"},
		{"forbidden", NewForbiddenError("m"), ErrForbidden, http.StatusForbidden, "m"},
		{"not found", NewNotFoundError("m"), ErrNotFound, http.StatusNotFound, "m"},
		{"conflict", NewConflictError("m"), ErrConflict, http.StatusConflict, "m"},
		{"validation", NewValidationError("m"), ErrValidation, http.StatusBadRequest, "m"},
		{
			"tenant not found", NewTenantNotFoundError(),
			ErrTenantNotFound, http.StatusNotFound, "空间不存在",
		},
		{
			"tenant already exists", NewTenantAlreadyExistsError(),
			ErrTenantAlreadyExists, http.StatusConflict, "空间已存在",
		},
		{
			"tenant inactive", NewTenantInactiveError(),
			ErrTenantInactive, http.StatusForbidden, "空间已停用",
		},
		{
			"tenant creation disabled", NewTenantCreationDisabledError(),
			ErrTenantCreationDisabled, http.StatusForbidden,
			"self-service workspace creation is disabled; join a workspace by invitation",
		},
		{
			"agent missing thinking model", NewAgentMissingThinkingModelError(),
			ErrAgentMissingThinkingModel, http.StatusBadRequest, "启用Agent模式前，请先选择思考模型",
		},
		{
			"agent missing allowed tools", NewAgentMissingAllowedToolsError(),
			ErrAgentMissingAllowedTools, http.StatusBadRequest, "至少需要选择一个允许的工具",
		},
		{
			"agent invalid max iterations", NewAgentInvalidMaxIterationsError(),
			ErrAgentInvalidMaxIterations, http.StatusBadRequest, "最大迭代次数必须在1-20之间",
		},
		{
			"agent invalid temperature", NewAgentInvalidTemperatureError(),
			ErrAgentInvalidTemperature, http.StatusBadRequest, "温度参数必须在0-2之间",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantCode, tt.err.Code)
			assert.Equal(t, tt.wantHTTP, tt.err.HTTPCode)
			assert.Equal(t, tt.wantMsg, tt.err.Message)
		})
	}
}

// TestConstructorsWithDefaultMessage covers the constructors that substitute a
// default message when given an empty string, but pass through a custom one.
func TestConstructorsWithDefaultMessage(t *testing.T) {
	tests := []struct {
		name       string
		build      func(string) *AppError
		wantCode   ErrorCode
		wantHTTP   int
		defaultMsg string
	}{
		{
			"too many requests", NewTooManyRequestsError,
			ErrTooManyRequests, http.StatusTooManyRequests, "too many requests",
		},
		{
			"internal server", NewInternalServerError,
			ErrInternalServer, http.StatusInternalServerError, "服务器内部错误",
		},
		{
			"service unavailable", NewServiceUnavailableError,
			ErrServiceUnavailable, http.StatusServiceUnavailable, "服务暂时不可用",
		},
		{
			"vector store binding invalid", NewVectorStoreBindingInvalidError,
			ErrVectorStoreBindingInvalid, http.StatusBadRequest, "vector store not found",
		},
		{
			"vector store unavailable", NewVectorStoreUnavailableError,
			ErrVectorStoreUnavailable, http.StatusBadRequest, "vector store is currently unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Empty message falls back to the default.
			empty := tt.build("")
			assert.Equal(t, tt.wantCode, empty.Code)
			assert.Equal(t, tt.wantHTTP, empty.HTTPCode)
			assert.Equal(t, tt.defaultMsg, empty.Message)

			// Custom message passes through unchanged.
			custom := tt.build("custom message")
			assert.Equal(t, "custom message", custom.Message)
		})
	}
}

func TestIsAppError(t *testing.T) {
	appErr := NewNotFoundError("missing")

	got, ok := IsAppError(appErr)
	require.True(t, ok)
	assert.Same(t, appErr, got)

	got, ok = IsAppError(stderrors.New("plain error"))
	assert.False(t, ok)
	assert.Nil(t, got)

	// A wrapped AppError is not detected: IsAppError uses a direct type
	// assertion, not errors.As.
	wrapped := fmt.Errorf("context: %w", appErr)
	got, ok = IsAppError(wrapped)
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestErrorCodesAreDistinct(t *testing.T) {
	codes := []ErrorCode{
		ErrBadRequest, ErrUnauthorized, ErrForbidden, ErrNotFound,
		ErrMethodNotAllowed, ErrConflict, ErrTooManyRequests, ErrInternalServer,
		ErrServiceUnavailable, ErrTimeout, ErrValidation,
		ErrTenantNotFound, ErrTenantAlreadyExists, ErrTenantInactive,
		ErrTenantNameRequired, ErrTenantInvalidStatus, ErrTenantCreationDisabled,
		ErrAgentMissingThinkingModel, ErrAgentMissingAllowedTools,
		ErrAgentInvalidMaxIterations, ErrAgentInvalidTemperature,
		ErrVectorStoreBindingInvalid, ErrVectorStoreUnavailable,
	}
	seen := make(map[ErrorCode]bool, len(codes))
	for _, c := range codes {
		assert.Falsef(t, seen[c], "duplicate error code %d", c)
		seen[c] = true
	}
}
