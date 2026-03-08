package arc

import (
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
)

// Response is typed HTTP response envelope.
type Response[T any] struct {
	Status  int
	Headers http.Header
	Body    *T
}

// JSON returns custom status JSON response.
func JSON[T any](status int, body T) *Response[T] {
	return &Response[T]{Status: status, Headers: make(http.Header), Body: &body}
}

// OK returns 200 response.
func OK[T any](body T) *Response[T] {
	return JSON(http.StatusOK, body)
}

// Created returns 201 response.
func Created[T any](body T) *Response[T] {
	return JSON(http.StatusCreated, body)
}

// NoContent returns 204 response.
func NoContent() *Response[struct{}] {
	return &Response[struct{}]{Status: http.StatusNoContent, Headers: make(http.Header)}
}

// File returns raw file download response.
func File(status int, filename string, content []byte) *RawResponse {
	if status == 0 {
		status = http.StatusOK
	}
	contentType := mime.TypeByExtension(filepath.Ext(filename))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	headers := make(http.Header)
	headers.Set("Content-Type", contentType)
	if filename != "" {
		headers.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(filename)))
	}
	return &RawResponse{
		Status:  status,
		Headers: headers,
		WriteTo: func(w http.ResponseWriter) error {
			_, err := w.Write(content)
			return err
		},
	}
}
