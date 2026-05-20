package modelgw_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func validChat() modelgw.ChatRequest {
	return modelgw.ChatRequest{
		Model: "openai:gpt-4o",
		Messages: []modelgw.ChatMessage{
			{Role: modelgw.RoleUser, Content: "hi"},
		},
	}
}

func TestValidateChatRequest_OK(t *testing.T) {
	require.NoError(t, modelgw.ValidateChatRequest(validChat()))
}

func TestValidateChatRequest_BadModel(t *testing.T) {
	r := validChat()
	r.Model = "no-prefix"
	require.ErrorIs(t, modelgw.ValidateChatRequest(r), modelgw.ErrModelInvalid)
}

func TestValidateChatRequest_EmptyMessages(t *testing.T) {
	r := validChat()
	r.Messages = nil
	require.Error(t, modelgw.ValidateChatRequest(r))
}

func TestValidateChatRequest_TooManyMessages(t *testing.T) {
	r := validChat()
	r.Messages = make([]modelgw.ChatMessage, modelgw.MaxMessages+1)
	for i := range r.Messages {
		r.Messages[i] = modelgw.ChatMessage{Role: modelgw.RoleUser, Content: "x"}
	}
	require.Error(t, modelgw.ValidateChatRequest(r))
}

func TestValidateChatRequest_MessageTooLarge(t *testing.T) {
	r := validChat()
	r.Messages[0].Content = string(make([]byte, modelgw.MaxMessageBytes+1))
	require.Error(t, modelgw.ValidateChatRequest(r))
}

func TestValidateEmbeddingsRequest_OK(t *testing.T) {
	require.NoError(t, modelgw.ValidateEmbeddingsRequest(modelgw.EmbeddingsRequest{
		Model: "openai:text-embedding-3-small",
		Input: []string{"hi"},
	}))
}

func TestValidateEmbeddingsRequest_BadModel(t *testing.T) {
	r := modelgw.EmbeddingsRequest{Model: "noprefix", Input: []string{"hi"}}
	require.ErrorIs(t, modelgw.ValidateEmbeddingsRequest(r), modelgw.ErrModelInvalid)
}

func TestValidateEmbeddingsRequest_EmptyInput(t *testing.T) {
	r := modelgw.EmbeddingsRequest{Model: "openai:x", Input: nil}
	require.Error(t, modelgw.ValidateEmbeddingsRequest(r))
}

func TestValidateEmbeddingsRequest_TooManyInputs(t *testing.T) {
	in := make([]string, modelgw.MaxEmbeddingInput+1)
	for i := range in {
		in[i] = "x"
	}
	r := modelgw.EmbeddingsRequest{Model: "openai:x", Input: in}
	require.Error(t, modelgw.ValidateEmbeddingsRequest(r))
}

func TestValidateEmbeddingsRequest_ItemTooLarge(t *testing.T) {
	r := modelgw.EmbeddingsRequest{Model: "openai:x",
		Input: []string{string(make([]byte, modelgw.MaxEmbeddingItem+1))}}
	require.Error(t, modelgw.ValidateEmbeddingsRequest(r))
}
