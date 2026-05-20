package modelgw

import (
	"fmt"
	"strings"
)

// ValidateChatRequest 校验 ChatRequest 必填与上限。
func ValidateChatRequest(r ChatRequest) error {
	if err := validateModelString(r.Model); err != nil {
		return err
	}
	if len(r.Messages) == 0 {
		return fmt.Errorf("validation: messages required")
	}
	if len(r.Messages) > MaxMessages {
		return fmt.Errorf("validation: messages count %d > %d", len(r.Messages), MaxMessages)
	}
	for i, m := range r.Messages {
		if len(m.Content) > MaxMessageBytes {
			return fmt.Errorf("validation: messages[%d].content %d > %d bytes", i, len(m.Content), MaxMessageBytes)
		}
	}
	return nil
}

// ValidateEmbeddingsRequest 校验 EmbeddingsRequest。
func ValidateEmbeddingsRequest(r EmbeddingsRequest) error {
	if err := validateModelString(r.Model); err != nil {
		return err
	}
	if len(r.Input) == 0 {
		return fmt.Errorf("validation: input required")
	}
	if len(r.Input) > MaxEmbeddingInput {
		return fmt.Errorf("validation: input count %d > %d", len(r.Input), MaxEmbeddingInput)
	}
	for i, s := range r.Input {
		if len(s) > MaxEmbeddingItem {
			return fmt.Errorf("validation: input[%d] %d > %d bytes", i, len(s), MaxEmbeddingItem)
		}
	}
	return nil
}

func validateModelString(s string) error {
	i := strings.IndexByte(s, ':')
	if i <= 0 || i == len(s)-1 {
		return ErrModelInvalid
	}
	return nil
}
