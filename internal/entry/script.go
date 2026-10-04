package entry

import "strings"

// The line form of an entry: one segment per line, kind by the line's first
// character. Narration has no mark. This is how a script reads, in prompts
// and on screen; the model answers in JSON under Schema.
var marks = map[Kind][2]string{
	Action:  {"(", ")"},
	Speech:  {`"`, `"`},
	Thought: {"`", "`"},
}

// FormatScript writes the line form.
func FormatScript(segments []Segment) string {
	lines := make([]string, 0, len(segments))
	for _, s := range segments {
		if m, ok := marks[s.Kind]; ok {
			lines = append(lines, m[0]+s.Text+m[1])
		} else {
			lines = append(lines, s.Text)
		}
	}
	return strings.Join(lines, "\n")
}

// Schema is the shape of a beat as a model answers it: one to five segments,
// each a kind and a text. Finite, because a model given an open end does not stop.
func Schema() map[string]any {
	return map[string]any{
		"type":     "array",
		"minItems": 1,
		"maxItems": 5,
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"kind": map[string]any{"enum": []string{string(Narration), string(Action), string(Speech), string(Thought)}},
				"text": map[string]any{"type": "string", "minLength": 1},
			},
			"required":             []string{"kind", "text"},
			"additionalProperties": false,
		},
	}
}
