package llm

import (
	"encoding/json"
	"strings"
)

// ExtractJSON parses raw model output into v. It tolerates responses that
// wrap the JSON payload in markdown code fences (```json ... ```), which
// providers without a native JSON mode sometimes emit despite being told
// not to.
func ExtractJSON(raw string, v any) error {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	return json.Unmarshal([]byte(raw), v)
}
