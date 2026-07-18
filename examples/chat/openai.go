package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
)

// llmCompletion is the OpenAI chat.completions call (same model as the JS express demo).
// It takes context.Context first so usageflow go build instruments it and captures
// resultSchema / usage / aiModel from the OpenAI response.
func llmCompletion(ctx context.Context, prompt string) (*openai.ChatCompletion, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is not set")
	}

	client := openai.NewClient(option.WithAPIKey(apiKey))
	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
		Model: openai.ChatModelGPT4oMini,
	})
	if err != nil {
		return nil, err
	}
	return completion, nil
}

func openaiConfigured() bool {
	return strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) != ""
}
