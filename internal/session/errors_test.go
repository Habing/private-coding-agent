package session

import "testing"

func TestErrorSentinelsNonNil(t *testing.T) {
	for _, e := range []error{
		ErrSessionNotFound, ErrSessionArchived, ErrEmptyContent, ErrModelRequired,
	} {
		if e == nil {
			t.Fatal("sentinel is nil")
		}
		if e.Error() == "" {
			t.Fatalf("sentinel has empty message: %T", e)
		}
	}
}
