package session

import "errors"

// Error sentinels exposed by the session package. Handler layers map these to
// HTTP status codes; WSHandler maps them to close codes.
var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionArchived = errors.New("session is archived")
	ErrEmptyContent    = errors.New("message content is empty")
	ErrModelRequired   = errors.New("model required")
)
