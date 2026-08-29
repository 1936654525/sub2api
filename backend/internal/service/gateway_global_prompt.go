package service

import (
	"context"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// injectGlobalSystemPrompt injects a global system prompt into the request
// body before it is forwarded upstream, dispatching on the request protocol:
//
//   - OpenAI chat/completions (messages): prepends {role:"system",content:...}.
//   - Anthropic messages (system): prepends a text block to system.
//   - Responses API (input): prepends a system message to input.
//   - Gemini (systemInstruction.parts): prepends a text part.
//
// The injection is skipped when the prompt is empty or the body has no
// recognizable chat structure.
func injectGlobalSystemPrompt(body []byte, protocol, prompt string) []byte {
	if len(body) == 0 || prompt == "" || !gjson.ValidBytes(body) {
		return body
	}

	switch strings.ToLower(protocol) {
	case PlatformAnthropic:
		return prependAnthropicSystemBlock(body, prompt)
	case PlatformGemini:
		return prependGeminiSystemPart(body, prompt)
	case "responses":
		return prependResponsesSystemMessage(body, prompt)
	default:
		if gjson.GetBytes(body, "input").IsArray() {
			return prependResponsesSystemMessage(body, prompt)
		}
		if gjson.GetBytes(body, "messages").IsArray() {
			return prependOpenAISystemMessage(body, prompt)
		}
		return body
	}
}

// prependOpenAISystemMessage inserts a system message at the front of the
// messages array, preserving the rest of the body.
func prependOpenAISystemMessage(body []byte, prompt string) []byte {
	msgs := gjson.GetBytes(body, "messages").Array()
	items := make([]any, 0, len(msgs)+1)
	items = append(items, map[string]any{"role": "system", "content": prompt})
	for _, m := range msgs {
		items = append(items, m.Value())
	}
	next, err := sjson.Set(string(body), "messages", items)
	if err != nil {
		return body
	}
	return []byte(next)
}

// prependAnthropicSystemBlock inserts a text block at the front of the system
// array, or creates the system array when absent. When the client sent a
// string system (Anthropic accepts both), it is normalized into the block
// array so the injected prompt becomes the first block.
func prependAnthropicSystemBlock(body []byte, prompt string) []byte {
	sys := gjson.GetBytes(body, "system")
	var blocks []any
	switch {
	case sys.IsArray():
		for _, b := range sys.Array() {
			blocks = append(blocks, b.Value())
		}
	case sys.Type == gjson.String:
		blocks = append(blocks, map[string]any{"type": "text", "text": sys.String()})
	}
	blocks = append([]any{map[string]any{"type": "text", "text": prompt}}, blocks...)
	next, err := sjson.Set(string(body), "system", blocks)
	if err != nil {
		return body
	}
	return []byte(next)
}

// prependResponsesSystemMessage inserts a system message at the front of the
// Responses API input array.
func prependResponsesSystemMessage(body []byte, prompt string) []byte {
	input := gjson.GetBytes(body, "input").Array()
	items := make([]any, 0, len(input)+1)
	items = append(items, map[string]any{"role": "system", "content": prompt})
	for _, m := range input {
		items = append(items, m.Value())
	}
	next, err := sjson.Set(string(body), "input", items)
	if err != nil {
		return body
	}
	return []byte(next)
}

// prependGeminiSystemPart inserts a text part at the front of
// systemInstruction.parts, creating the structure when absent.
func prependGeminiSystemPart(body []byte, prompt string) []byte {
	si := gjson.GetBytes(body, "systemInstruction")
	if si.Exists() {
		parts := si.Get("parts").Array()
		items := make([]any, 0, len(parts)+1)
		items = append(items, map[string]any{"text": prompt})
		for _, p := range parts {
			items = append(items, p.Value())
		}
		next, err := sjson.Set(string(body), "systemInstruction.parts", items)
		if err != nil {
			return body
		}
		return []byte(next)
	}
	next, err := sjson.Set(string(body), "systemInstruction.parts", []any{map[string]any{"text": prompt}})
	if err != nil {
		return body
	}
	return []byte(next)
}

// injectGlobalSystemPromptIfEnabled reads the global prompt settings and
// injects the prompt into the request body when both the switch and the prompt
// are set. Returns the (possibly unchanged) body. A nil setting service or nil
// receiver makes this a no-op.
func injectGlobalSystemPromptIfEnabled(ss *SettingService, ctx context.Context, protocol string, body []byte) []byte {
	if ss == nil {
		return body
	}
	enabled, prompt := ss.GetGlobalSystemPromptSettings(ctx)
	if !enabled || prompt == "" {
		return body
	}
	return injectGlobalSystemPrompt(body, protocol, prompt)
}

// (s *GatewayService) injectGlobalSystemPrompt wraps the package-level helper
// for the main forwarding path.
func (s *GatewayService) injectGlobalSystemPrompt(ctx context.Context, protocol string, body []byte) []byte {
	if s == nil {
		return body
	}
	return injectGlobalSystemPromptIfEnabled(s.settingService, ctx, protocol, body)
}