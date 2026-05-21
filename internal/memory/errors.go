package memory

import "errors"

// Error sentinels exposed by the memory package. Handler layers map these to
// HTTP status codes; tools map them to error payloads.
var (
	ErrMemoryNotFound    = errors.New("memory not found")
	ErrEmptyContent      = errors.New("content required")
	ErrInvalidType       = errors.New("invalid memory type")
	ErrEmptySearch       = errors.New("search requires at least one of query, type, tags")
	ErrInvalidSearchMode = errors.New("invalid search mode")
	ErrVectorDisabled    = errors.New("vector search not available")
)
