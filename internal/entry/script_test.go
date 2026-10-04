package entry

import (
	"reflect"
	"strings"
	"testing"
)

func TestScriptRoundTrip(t *testing.T) {
	in := "(sets a cup down)\n\"You said three days.\"\n\n`I cannot lie for her.`\nRain on the shutters.\n\"unterminated\n"
	want := []Segment{
		{Kind: Action, Text: "sets a cup down"},
		{Kind: Speech, Text: "You said three days."},
		{Kind: Thought, Text: "I cannot lie for her."},
		{Kind: Narration, Text: "Rain on the shutters."},
		{Kind: Speech, Text: "unterminated"},
	}
	got := ParseScript(in)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parse = %+v\nwant %+v", got, want)
	}
	if again := ParseScript(FormatScript(got)); !reflect.DeepEqual(again, want) {
		t.Fatalf("round trip = %+v", again)
	}
	if ParseScript("  \n\n") != nil {
		t.Fatal("blank input should parse to nothing")
	}
}

func TestGrammar(t *testing.T) {
	if !strings.HasPrefix(Grammar, "root ::= line (line (line (line line?)?)?)?\n") {
		t.Fatalf("grammar root: %s", Grammar)
	}
	for _, rule := range []string{"action ::=", "speech ::=", "thought ::=", "narration ::="} {
		if !strings.Contains(Grammar, rule) {
			t.Fatalf("grammar missing %s:\n%s", rule, Grammar)
		}
	}
}
