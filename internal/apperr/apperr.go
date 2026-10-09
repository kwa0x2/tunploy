// Package apperr is an error a caller can act on. It says what kind of
// failure happened, never an HTTP status, so the services that return it do
// not depend on how they are reached.
package apperr

import (
	"fmt"
	"maps"
)

type Kind uint8

const (
	_ Kind = iota
	// Invalid input; Fields says which values to fix.
	Invalid
	NotFound
	// The request clashes with the current state, such as a full subnet.
	Conflict
	// Something the work needs is down for now, so a retry may work.
	Unavailable
	// Something the work needs refused it, such as Docker failing a deploy.
	Upstream
)

type Error struct {
	Kind    Kind
	Code    string
	Message string
	Fields  map[string]string
	// The cause, for errors.Is and errors.As; it never reaches the client.
	Err error
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func (e *Error) Unwrap() error { return e.Err }

func New(kind Kind, code, format string, args ...any) *Error {
	return &Error{Kind: kind, Code: code, Message: fmt.Sprintf(format, args...)}
}

// Fields is a validation failure, or nil when every map is empty. A later
// map wins on the same field, so a parse error can replace a vaguer check.
func Fields(fields ...map[string]string) error {
	all := map[string]string{}
	for _, f := range fields {
		maps.Copy(all, f)
	}
	if len(all) == 0 {
		return nil
	}
	return &Error{Kind: Invalid, Code: "validation_failed", Message: "some fields are invalid", Fields: all}
}

func Field(name, format string, args ...any) error {
	return Fields(map[string]string{name: fmt.Sprintf(format, args...)})
}
