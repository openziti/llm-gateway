package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// APIError represents an OpenAI-compatible API error.
type APIError struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param,omitempty"`
	Code    *string `json:"code,omitempty"`
}

// ErrorResponse wraps an APIError in the standard response format.
type ErrorResponse struct {
	Error APIError `json:"error"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return e.Message
}

// NewAPIError creates a new APIError.
func NewAPIError(message, errType string) *APIError {
	return &APIError{
		Message: message,
		Type:    errType,
	}
}

// NewAPIErrorWithCode creates a new APIError with a code.
func NewAPIErrorWithCode(message, errType, code string) *APIError {
	return &APIError{
		Message: message,
		Type:    errType,
		Code:    &code,
	}
}

// common error types
const (
	ErrorTypeInvalidRequest     = "invalid_request_error"
	ErrorTypeAuthentication     = "authentication_error"
	ErrorTypePermission         = "permission_error"
	ErrorTypeNotFound           = "not_found_error"
	ErrorTypeRateLimit          = "rate_limit_error"
	ErrorTypeServer             = "server_error"
	ErrorTypeServiceUnavailable = "service_unavailable"
)

// predefined errors
var (
	ErrInvalidJSON      = NewAPIError("invalid JSON in request body", ErrorTypeInvalidRequest)
	ErrModelRequired    = NewAPIError("model is required", ErrorTypeInvalidRequest)
	ErrMessagesRequired = NewAPIError("messages is required", ErrorTypeInvalidRequest)
	ErrUnauthorized     = NewAPIError("invalid API key", ErrorTypeAuthentication)
)

// ErrModelNotFound creates a model not found error.
func ErrModelNotFound(model string) *APIError {
	return NewAPIError(fmt.Sprintf("model '%s' not found", model), ErrorTypeNotFound)
}

// ErrProviderNotConfigured creates a provider not configured error.
func ErrProviderNotConfigured(provider string) *APIError {
	return NewAPIError(fmt.Sprintf("provider '%s' is not configured", provider), ErrorTypeInvalidRequest)
}

// ErrProviderError creates a provider-specific error.
func ErrProviderError(message string) *APIError {
	return NewAPIError(message, ErrorTypeServer)
}

// ErrRateLimit creates a rate limit error.
func ErrRateLimit(message string) *APIError {
	return NewAPIError(message, ErrorTypeRateLimit)
}

// WriteError writes an error response with the appropriate status code.
func WriteError(w http.ResponseWriter, err *APIError, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(ErrorResponse{Error: *err})
}

// StatusCodeForError returns the appropriate HTTP status code for an error type.
func StatusCodeForError(errType string) int {
	switch errType {
	case ErrorTypeInvalidRequest:
		return http.StatusBadRequest
	case ErrorTypeAuthentication:
		return http.StatusUnauthorized
	case ErrorTypePermission:
		return http.StatusForbidden
	case ErrorTypeNotFound:
		return http.StatusNotFound
	case ErrorTypeRateLimit:
		return http.StatusTooManyRequests
	case ErrorTypeServiceUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
