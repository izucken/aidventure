package entry

import "strings"

// The line form of an entry: one segment per line, kind by the line's first
// character. Narration has no mark. The same grammar the composer uses, and
// the one a model is asked to write in.
var marks = map[Kind][2]string{
	Action:  {"(", ")"},
	Speech:  {`"`, `"`},
	Thought: {"`", "`"},
}

// ParseScript reads the line form. Closing marks are optional. Blank lines are skipped.
func ParseScript(text string) []Segment {
	var segments []Segment
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		kind := Narration
		for k, m := range marks {
			if strings.HasPrefix(line, m[0]) {
				kind = k
				line = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, m[0]), m[1]))
				break
			}
		}
		if line != "" {
			segments = append(segments, Segment{Kind: kind, Text: line})
		}
	}
	return segments
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

// Grammar constrains a model to the line form in llama.cpp's GBNF: one to
// five lines. Finite, because a model given an open end does not stop; the
// sampler ends the entry when the grammar is complete. Narration may not
// open with a mark or a cue-like character, so every line reads as one kind.
const Grammar = "root ::= line (line (line (line line?)?)?)?\n" + `line ::= (action | speech | thought | narration) "\n"
action ::= "(" [^\n)]+ ")"
speech ::= "\"" [^\n"]+ "\""
thought ::= "` + "`" + `" [^\n` + "`" + `]+ "` + "`" + `"
narration ::= [^\n("` + "`" + ` <*\-=] [^\n]*
`
