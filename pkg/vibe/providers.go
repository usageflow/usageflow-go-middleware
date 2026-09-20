package vibe

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

const (
	defaultAnthropicURL = "https://api.anthropic.com"
	defaultOpenAIURL    = "https://api.openai.com"
	anthropicVersion    = "2023-06-01"
)

func inferProvider(model string) Provider {
	if strings.HasPrefix(model, "claude") {
		return ProviderAnthropic
	}
	return ProviderOpenAI
}

// OpenAI reasoning models reject max_tokens and non-default temperature.
func isOpenAIReasoningModel(model string) bool {
	for _, p := range []string{"o1", "o3", "o4", "gpt-5"} {
		if strings.HasPrefix(model, p) {
			return true
		}
	}
	return false
}

func (c *Client) postJSON(ctx context.Context, url string, headers map[string]string, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("provider returned %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return resp, nil
}

func (c *Client) anthropicHeaders() (map[string]string, error) {
	if c.opts.AnthropicAPIKey == "" {
		return nil, fmt.Errorf("usageflow vibe: no Anthropic API key — set ANTHROPIC_API_KEY or Options.AnthropicAPIKey")
	}
	return map[string]string{"x-api-key": c.opts.AnthropicAPIKey, "anthropic-version": anthropicVersion}, nil
}

func (c *Client) openAIHeaders() (map[string]string, error) {
	if c.opts.OpenAIAPIKey == "" {
		return nil, fmt.Errorf("usageflow vibe: no OpenAI API key — set OPENAI_API_KEY or Options.OpenAIAPIKey")
	}
	return map[string]string{"Authorization": "Bearer " + c.opts.OpenAIAPIKey}, nil
}

func anthropicBody(r *ChatRequest, maxTokens int, stream bool) map[string]any {
	var system string
	msgs := make([]Message, 0, len(r.Messages))
	for _, m := range r.Messages {
		if m.Role == "system" {
			if system == "" {
				system = m.Content
			}
			continue
		}
		msgs = append(msgs, m)
	}
	body := map[string]any{"model": r.Model, "max_tokens": maxTokens, "messages": msgs}
	if system != "" {
		body["system"] = system
	}
	if r.Temperature != nil {
		body["temperature"] = *r.Temperature
	}
	if len(r.Tools) > 0 {
		body["tools"] = r.Tools
	}
	if r.ToolChoice != nil {
		body["tool_choice"] = r.ToolChoice
	}
	if stream {
		body["stream"] = true
	}
	return body
}

func openAIBody(r *ChatRequest, maxTokens int, stream bool) map[string]any {
	body := map[string]any{"model": r.Model, "messages": r.Messages}
	if isOpenAIReasoningModel(r.Model) {
		body["max_completion_tokens"] = maxTokens
	} else {
		body["max_tokens"] = maxTokens
		if r.Temperature != nil {
			body["temperature"] = *r.Temperature
		}
	}
	if len(r.Tools) > 0 {
		body["tools"] = r.Tools
	}
	if r.ToolChoice != nil {
		body["tool_choice"] = r.ToolChoice
	}
	if stream {
		body["stream"] = true
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	return body
}

func str(v any) string { s, _ := v.(string); return s }

func num(v any) int { f, _ := v.(float64); return int(f) }

func parseJSONLoose(raw string) any {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	return v
}

func (c *Client) dispatch(ctx context.Context, r *ChatRequest, maxTokens int) (*ChatResult, error) {
	if r.Provider == ProviderAnthropic {
		return c.dispatchAnthropic(ctx, r, maxTokens)
	}
	return c.dispatchOpenAI(ctx, r, maxTokens)
}

func (c *Client) dispatchAnthropic(ctx context.Context, r *ChatRequest, maxTokens int) (*ChatResult, error) {
	h, err := c.anthropicHeaders()
	if err != nil {
		return nil, err
	}
	resp, err := c.postJSON(ctx, c.opts.AnthropicBaseURL+"/v1/messages", h, anthropicBody(r, maxTokens, false))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	res := &ChatResult{Provider: ProviderAnthropic, Model: r.Model, RequestedModel: r.Model, Raw: raw}
	if m := str(raw["model"]); m != "" {
		res.Model = m
	}
	var text strings.Builder
	blocks, _ := raw["content"].([]any)
	for _, b := range blocks {
		blk, _ := b.(map[string]any)
		switch str(blk["type"]) {
		case "text":
			text.WriteString(str(blk["text"]))
		case "tool_use":
			res.ToolCalls = append(res.ToolCalls, ToolCall{ID: str(blk["id"]), Name: str(blk["name"]), Input: blk["input"]})
		}
	}
	res.Content = text.String()
	if u, ok := raw["usage"].(map[string]any); ok {
		res.Usage = Usage{InputTokens: num(u["input_tokens"]), OutputTokens: num(u["output_tokens"])}
	}
	return res, nil
}

func (c *Client) dispatchOpenAI(ctx context.Context, r *ChatRequest, maxTokens int) (*ChatResult, error) {
	h, err := c.openAIHeaders()
	if err != nil {
		return nil, err
	}
	resp, err := c.postJSON(ctx, c.opts.OpenAIBaseURL+"/v1/chat/completions", h, openAIBody(r, maxTokens, false))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	res := &ChatResult{Provider: ProviderOpenAI, Model: r.Model, RequestedModel: r.Model, Raw: raw}
	if m := str(raw["model"]); m != "" {
		res.Model = m
	}
	if choices, _ := raw["choices"].([]any); len(choices) > 0 {
		ch, _ := choices[0].(map[string]any)
		msg, _ := ch["message"].(map[string]any)
		res.Content = str(msg["content"])
		calls, _ := msg["tool_calls"].([]any)
		for _, tc := range calls {
			t, _ := tc.(map[string]any)
			fn, _ := t["function"].(map[string]any)
			res.ToolCalls = append(res.ToolCalls, ToolCall{ID: str(t["id"]), Name: str(fn["name"]), Input: parseJSONLoose(str(fn["arguments"]))})
		}
	}
	if u, ok := raw["usage"].(map[string]any); ok {
		res.Usage = Usage{InputTokens: num(u["prompt_tokens"]), OutputTokens: num(u["completion_tokens"])}
	}
	return res, nil
}

// sseEvents calls fn for each SSE `data:` payload until the stream ends or fn returns an error.
func sseEvents(body io.Reader, fn func(data string) error) error {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		if err := fn(data); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (c *Client) dispatchStream(ctx context.Context, r *ChatRequest, maxTokens int, onDelta func(string)) (*ChatResult, error) {
	if r.Provider == ProviderAnthropic {
		return c.streamAnthropic(ctx, r, maxTokens, onDelta)
	}
	return c.streamOpenAI(ctx, r, maxTokens, onDelta)
}

func (c *Client) streamAnthropic(ctx context.Context, r *ChatRequest, maxTokens int, onDelta func(string)) (*ChatResult, error) {
	h, err := c.anthropicHeaders()
	if err != nil {
		return nil, err
	}
	resp, err := c.postJSON(ctx, c.opts.AnthropicBaseURL+"/v1/messages", h, anthropicBody(r, maxTokens, true))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	res := &ChatResult{Provider: ProviderAnthropic, Model: r.Model, RequestedModel: r.Model}
	var text strings.Builder
	type toolAcc struct {
		id, name string
		args     strings.Builder
	}
	tools := map[int]*toolAcc{}
	err = sseEvents(resp.Body, func(data string) error {
		var ev map[string]any
		if json.Unmarshal([]byte(data), &ev) != nil {
			return nil
		}
		switch str(ev["type"]) {
		case "message_start":
			msg, _ := ev["message"].(map[string]any)
			if m := str(msg["model"]); m != "" {
				res.Model = m
			}
			if u, ok := msg["usage"].(map[string]any); ok {
				res.Usage.InputTokens = num(u["input_tokens"])
				res.Usage.OutputTokens = num(u["output_tokens"])
			}
		case "content_block_start":
			cb, _ := ev["content_block"].(map[string]any)
			if str(cb["type"]) == "tool_use" {
				tools[num(ev["index"])] = &toolAcc{id: str(cb["id"]), name: str(cb["name"])}
			}
		case "content_block_delta":
			d, _ := ev["delta"].(map[string]any)
			switch str(d["type"]) {
			case "text_delta":
				t := str(d["text"])
				text.WriteString(t)
				onDelta(t)
			case "input_json_delta":
				if a := tools[num(ev["index"])]; a != nil {
					a.args.WriteString(str(d["partial_json"]))
				}
			}
		case "message_delta":
			if u, ok := ev["usage"].(map[string]any); ok {
				res.Usage.OutputTokens = num(u["output_tokens"])
			}
		case "error":
			e, _ := ev["error"].(map[string]any)
			return fmt.Errorf("anthropic stream error: %s", str(e["message"]))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	res.Content = text.String()
	idxs := make([]int, 0, len(tools)) // indices are sparse: text blocks take slots too
	for i := range tools {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	for _, i := range idxs {
		a := tools[i]
		res.ToolCalls = append(res.ToolCalls, ToolCall{ID: a.id, Name: a.name, Input: parseJSONLoose(a.args.String())})
	}
	return res, nil
}

func (c *Client) streamOpenAI(ctx context.Context, r *ChatRequest, maxTokens int, onDelta func(string)) (*ChatResult, error) {
	h, err := c.openAIHeaders()
	if err != nil {
		return nil, err
	}
	resp, err := c.postJSON(ctx, c.opts.OpenAIBaseURL+"/v1/chat/completions", h, openAIBody(r, maxTokens, true))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	res := &ChatResult{Provider: ProviderOpenAI, Model: r.Model, RequestedModel: r.Model}
	var text strings.Builder
	type toolAcc struct {
		id, name string
		args     strings.Builder
	}
	tools := map[int]*toolAcc{}
	err = sseEvents(resp.Body, func(data string) error {
		var ev map[string]any
		if json.Unmarshal([]byte(data), &ev) != nil {
			return nil
		}
		if m := str(ev["model"]); m != "" {
			res.Model = m
		}
		if u, ok := ev["usage"].(map[string]any); ok {
			res.Usage = Usage{InputTokens: num(u["prompt_tokens"]), OutputTokens: num(u["completion_tokens"])}
		}
		choices, _ := ev["choices"].([]any)
		if len(choices) == 0 {
			return nil
		}
		ch, _ := choices[0].(map[string]any)
		d, _ := ch["delta"].(map[string]any)
		if t := str(d["content"]); t != "" {
			text.WriteString(t)
			onDelta(t)
		}
		calls, _ := d["tool_calls"].([]any)
		for _, tc := range calls {
			t, _ := tc.(map[string]any)
			idx := num(t["index"])
			a := tools[idx]
			if a == nil {
				a = &toolAcc{}
				tools[idx] = a
			}
			if id := str(t["id"]); id != "" {
				a.id = id
			}
			fn, _ := t["function"].(map[string]any)
			if n := str(fn["name"]); n != "" {
				a.name = n
			}
			a.args.WriteString(str(fn["arguments"]))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	res.Content = text.String()
	for i := 0; i < len(tools); i++ {
		if a := tools[i]; a != nil {
			res.ToolCalls = append(res.ToolCalls, ToolCall{ID: a.id, Name: a.name, Input: parseJSONLoose(a.args.String())})
		}
	}
	return res, nil
}

func (c *Client) dispatchEmbedding(ctx context.Context, model string, input []string) (*EmbedResult, error) {
	h, err := c.openAIHeaders()
	if err != nil {
		return nil, err
	}
	resp, err := c.postJSON(ctx, c.opts.OpenAIBaseURL+"/v1/embeddings", h, map[string]any{"model": model, "input": input})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw struct {
		Model string `json:"model"`
		Data  []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	res := &EmbedResult{Model: model, RequestedModel: model, Usage: Usage{InputTokens: raw.Usage.PromptTokens}}
	if raw.Model != "" {
		res.Model = raw.Model
	}
	for _, d := range raw.Data {
		res.Embeddings = append(res.Embeddings, d.Embedding)
	}
	return res, nil
}
