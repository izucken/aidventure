package entry

import "testing"

func TestFormatScript(t *testing.T) {
	got := FormatScript([]Segment{
		{Kind: Action, Text: "sets a cup down"},
		{Kind: Speech, Text: "You said three days."},
		{Kind: Thought, Text: "I cannot lie for her."},
		{Kind: Narration, Text: "Rain on the shutters."},
	})
	if got != "(sets a cup down)\n\"You said three days.\"\n`I cannot lie for her.`\nRain on the shutters." {
		t.Fatalf("format = %q", got)
	}
}

func TestSchema(t *testing.T) {
	s := Schema()
	items, _ := s["items"].(map[string]any)
	if s["type"] != "array" || s["maxItems"] != 5 || items == nil || items["additionalProperties"] != false {
		t.Fatalf("schema = %+v", s)
	}
}
