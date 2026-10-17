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

// OpenAI reasoning models reject max_tokens and non-default temperature. Keep this in sync with
// the SupportsTemperature:false entries in ledger/pkg/types/model_registry.go (that registry is
// the source of truth for per-model capabilities; this is a local, provider-call-shape mirror of
// it since vibe doesn't depend on ledger).
func isOpenAIReasoningModel(model string) bool {
	for _, p := range []string{"o1", "o3", "o4", "gpt-5", "gpt-6"} {
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
				system = contentText(m.Content)
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
	if r.OutputConfig != nil {
		body["output_config"] = r.OutputConfig
	}
	if stream {
		body["stream"] = true
	}
	return body
}

func openAIBody(r *ChatRequest, maxTokens int, stream bool) map[string]any {
	body := map[string]any{"model": r.Model, "messages": openAIMessages(r.Messages)}
	tools := openAITools(r.Tools)
	if isOpenAIReasoningModel(r.Model) {
		body["max_completion_tokens"] = maxTokens
		// /v1/chat/completions rejects a reasoning model's default (non-"none") reasoning_effort
		// the moment tools are attached ("Function tools with reasoning_effort are not supported
		// for <model> ... set reasoning_effort to 'none'"); verified live against gpt-6-luna.
		if len(tools) > 0 {
			body["reasoning_effort"] = "none"
		}
	} else {
		body["max_tokens"] = maxTokens
		if r.Temperature != nil {
			body["temperature"] = *r.Temperature
		}
	}
	if tools != nil {
		body["tools"] = tools
	}
	if tc := openAIToolChoice(r.ToolChoice); tc != nil {
		body["tool_choice"] = tc
	}
	if rf := openAIResponseFormat(r.OutputConfig); rf != nil {
		body["response_format"] = rf
	}
	if stream {
		body["stream"] = true
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	return body
}

// openAIResponseFormat translates the Anthropic-shaped OutputConfig (passed through untouched in
// anthropicBody as output_config: {"format": {"type": "json_schema", "schema": {...}}, ...}) into
// OpenAI's chat-completions response_format, so a schema-constrained call still gets structured
// output after a policy routes it from Claude to an OpenAI model. Only the json_schema case is
// translated (the only one callers use today); anything else in OutputConfig is dropped rather
// than sent to OpenAI malformed. Not run in strict mode: callers' schemas (e.g. fitapp's
// partialObject) mark only some properties required, which OpenAI's strict mode rejects, and
// non-strict is still enough to bias the model toward the given shape.
func openAIResponseFormat(outputConfig any) map[string]any {
	cfg, ok := outputConfig.(map[string]any)
	if !ok {
		return nil
	}
	format, ok := cfg["format"].(map[string]any)
	if !ok || str(format["type"]) != "json_schema" {
		return nil
	}
	schema, ok := format["schema"]
	if !ok {
		return nil
	}
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "response",
			"schema": schema,
		},
	}
}

// contentBlocks normalizes a Message.Content holding Anthropic-style content blocks to
// []map[string]any, regardless of which concrete slice type the caller built it as — chat.go
// builds assistant/tool-result blocks as []map[string]any (e.g. assistantBlocks, resultBlocks in
// fitapp/internal/coach/chat.go), while content decoded from JSON (e.g. round-tripped through
// toVibeMessages) comes back as []any of map[string]any. A bare type assertion to either one alone
// silently misses the other and falls through as an untranslated, provider-specific block array —
// exactly what reached OpenAI's API as a raw invalid "tool_use" content type before this existed.
func contentBlocks(c any) ([]map[string]any, bool) {
	switch v := c.(type) {
	case []map[string]any:
		return v, true
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out, true
	default:
		return nil, false
	}
}

// openAIMessages translates Anthropic-shaped message content (plain text, or a content-block
// array built by a tool-use or vision round: {"type":"text"|"image"|"tool_use"|"tool_result",
// ...}, the shape chat.go round-trips through Message.Content so one call site works for either
// provider) into OpenAI's chat-completions message shape. A message carrying tool_result blocks
// has no OpenAI equivalent as a single message — each becomes its own role:"tool" message — so
// it expands to several entries; everything else maps one-to-one. A text-only message collapses
// to a plain string (not a one-part array) since that's the shape every existing caller/test
// already sends and expects back.
func openAIMessages(msgs []Message) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		blocks, ok := contentBlocks(m.Content)
		if !ok {
			out = append(out, map[string]any{"role": m.Role, "content": m.Content})
			continue
		}
		var parts []map[string]any
		var toolCalls []map[string]any
		var toolResults []map[string]any
		for _, blk := range blocks {
			switch str(blk["type"]) {
			case "text":
				parts = append(parts, map[string]any{"type": "text", "text": str(blk["text"])})
			case "image":
				if url := openAIImageURL(blk["source"]); url != "" {
					parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
				}
			case "tool_use":
				args, _ := json.Marshal(blk["input"])
				toolCalls = append(toolCalls, map[string]any{
					"id": str(blk["id"]), "type": "function",
					"function": map[string]any{"name": str(blk["name"]), "arguments": string(args)},
				})
			case "tool_result":
				toolResults = append(toolResults, map[string]any{
					"role": "tool", "tool_call_id": str(blk["tool_use_id"]), "content": str(blk["content"]),
				})
			}
		}
		if len(toolResults) > 0 {
			out = append(out, toolResults...)
			continue
		}
		msg := map[string]any{"role": m.Role, "content": openAIContent(parts)}
		if len(toolCalls) > 0 {
			msg["tool_calls"] = toolCalls
		}
		out = append(out, msg)
	}
	return out
}

// openAIContent collapses a parts list to a plain string when it's a single text part (or
// empty) — the shape every text-only caller/test already sends and expects back — and otherwise
// returns the OpenAI multi-part content array (needed once an image part is present).
func openAIContent(parts []map[string]any) any {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 && str(parts[0]["type"]) == "text" {
		return str(parts[0]["text"])
	}
	return parts
}

// openAIImageURL translates an Anthropic image block's "source" ({"type": "base64",
// "media_type": ..., "data": ...} or {"type": "url", "url": ...}) into the single data: or
// https: URL string OpenAI's image_url part takes.
func openAIImageURL(source any) string {
	src, ok := source.(map[string]any)
	if !ok {
		return ""
	}
	switch str(src["type"]) {
	case "base64":
		mt, data := str(src["media_type"]), str(src["data"])
		if mt == "" || data == "" {
			return ""
		}
		return "data:" + mt + ";base64," + data
	case "url":
		return str(src["url"])
	}
	return ""
}

// openAITools translates Anthropic-shaped tool definitions ({"name", "description",
// "input_schema", "strict"}, the only shape callers build today — see toolSpec.raw() in
// fitapp/internal/coach/chat.go) into OpenAI's function-tool shape. Anything already in OpenAI
// shape (has a "type" key) passes through untouched.
func openAITools(tools []any) []any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		tm, ok := t.(map[string]any)
		if !ok {
			out = append(out, t)
			continue
		}
		if _, isOpenAIShape := tm["type"]; isOpenAIShape {
			out = append(out, t)
			continue
		}
		fn := map[string]any{"name": tm["name"], "description": tm["description"], "parameters": tm["input_schema"]}
		if strict, _ := tm["strict"].(bool); strict {
			fn["strict"] = true
		}
		out = append(out, map[string]any{"type": "function", "function": fn})
	}
	return out
}

// openAIToolChoice translates Anthropic-shaped tool_choice ({"type": "auto"|"any"|"none"|"tool",
// "name": ...}) into OpenAI's chat-completions tool_choice. A plain string (already OpenAI shape)
// passes through untouched.
func openAIToolChoice(toolChoice any) any {
	if toolChoice == nil {
		return nil
	}
	if s, ok := toolChoice.(string); ok {
		return s
	}
	m, ok := toolChoice.(map[string]any)
	if !ok {
		return toolChoice
	}
	switch str(m["type"]) {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "none":
		return "none"
	case "tool":
		return map[string]any{"type": "function", "function": map[string]any{"name": str(m["name"])}}
	}
	return toolChoice
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
