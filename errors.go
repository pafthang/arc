package arc

import (
	"net/http"
)

// ErrorDetail describes one validation/binding problem.
type ErrorDetail struct {
	Path    string `json:"path,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// APIError is standard error payload.
type APIError struct {
	Status    int           `json:"-"`
	Code      string        `json:"code"`
	Message   string        `json:"message"`
	Details   []ErrorDetail `json:"details,omitempty"`
	RequestID string        `json:"requestId,omitempty"`
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

// BadRequest creates 400 API error.
func BadRequest(code, message string) *APIError {
	return &APIError{Status: http.StatusBadRequest, Code: code, Message: message}
}

// Validation creates 422 API error.
func Validation(code, message string) *APIError {
	return &APIError{Status: http.StatusUnprocessableEntity, Code: code, Message: message}
}

func writeProblem(w http.ResponseWriter, status int, e *APIError, enc Encoder) {
	if e == nil {
		e = &APIError{Code: "internal_error", Message: "Internal server error"}
	}
	if status == 0 {
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/problem+json")
	_ = enc.Encode(w, status, e)
}
