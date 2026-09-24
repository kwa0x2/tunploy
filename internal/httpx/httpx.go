// Package httpx holds the JSON encoding, request decoding and error envelope
// shared by every handler.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

const maxBodyBytes = 1 << 20

type Error struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

func Errorf(status int, code, format string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

func BadRequest(format string, args ...any) *Error {
	return Errorf(http.StatusBadRequest, "invalid_request", format, args...)
}

func Unauthorized(format string, args ...any) *Error {
	return Errorf(http.StatusUnauthorized, "unauthorized", format, args...)
}

func Forbidden(format string, args ...any) *Error {
	return Errorf(http.StatusForbidden, "forbidden", format, args...)
}

func NotFound(format string, args ...any) *Error {
	return Errorf(http.StatusNotFound, "not_found", format, args...)
}

func Conflict(format string, args ...any) *Error {
	return Errorf(http.StatusConflict, "conflict", format, args...)
}

func Invalid(fields map[string]string) *Error {
	return &Error{
		Status:  http.StatusUnprocessableEntity,
		Code:    "validation_failed",
		Message: "some fields are invalid",
		Fields:  fields,
	}
}

// Handler is an http.Handler that reports failures by returning them.
type Handler func(w http.ResponseWriter, r *http.Request) error

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		WriteError(w, r, err)
	}
}

func JSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return nil
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Status line is already out; all we can do is record it.
		slog.Error("encode response", "error", err)
	}
	return nil
}

func NoContent(w http.ResponseWriter) error {
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func Decode(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return BadRequest("request body is empty")
		}
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return BadRequest("request body is too large")
		}
		return BadRequest("request body is not valid JSON: %v", err)
	}
	return nil
}

// WriteError reports anything that is not an *Error as a generic internal
// error, so implementation details never reach the client.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		slog.Error("request failed",
			"error", err,
			"method", r.Method,
			"path", r.URL.Path,
		)
		apiErr = Errorf(http.StatusInternalServerError, "internal_error", "something went wrong")
	}
	JSON(w, apiErr.Status, map[string]any{"error": apiErr})
}
