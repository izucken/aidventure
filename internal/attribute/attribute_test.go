package attribute_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"aidventure/internal/attribute"
	"aidventure/internal/game"
	"aidventure/internal/store"
	"aidventure/internal/subject"
)

func TestStore(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	games, attrs, subjects := game.NewStore(db), attribute.NewStore(db), subject.NewStore(db)

	if _, err := attrs.Create(ctx, "nope", attribute.Input{Name: "x", Kind: attribute.Flag}); !errors.Is(err, game.ErrNotFound) {
		t.Fatalf("create in unknown game: err = %v; want game.ErrNotFound", err)
	}
	g, err := games.Create(ctx, "game")
	if err != nil {
		t.Fatal(err)
	}
	for name, in := range map[string]attribute.Input{
		"blank name":     {Name: " ", Kind: attribute.Flag},
		"unknown kind":   {Name: "x", Kind: "text"},
		"enum no option": {Name: "x", Kind: attribute.Enum},
		"enum blank":     {Name: "x", Kind: attribute.Enum, Options: []string{"a", " "}},
		"enum twice":     {Name: "x", Kind: attribute.Enum, Options: []string{"a", "a "}},
	} {
		if _, err := attrs.Create(ctx, g.ID, in); !errors.Is(err, attribute.ErrBad) {
			t.Fatalf("%s: err = %v; want ErrBad", name, err)
		}
	}
	strength, err := attrs.Create(ctx, g.ID, attribute.Input{Name: " Strength ", Description: "Raw power.", Kind: attribute.Integer, Options: []string{"ignored"}})
	if err != nil {
		t.Fatal(err)
	}
	mood, err := attrs.Create(ctx, g.ID, attribute.Input{Name: "Mood", Kind: attribute.Enum, Options: []string{"calm", " angry "}})
	if err != nil {
		t.Fatal(err)
	}
	armed, err := attrs.Create(ctx, g.ID, attribute.Input{Name: "Armed", Kind: attribute.Flag})
	if err != nil {
		t.Fatal(err)
	}
	list, err := attrs.List(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Name != "Strength" || len(list[0].Options) != 0 || strings.Join(list[1].Options, ",") != "calm,angry" {
		t.Fatalf("list = %+v", list)
	}

	// A value must fit its declaration, and an owner carries a name once.
	for name, vs := range map[string][]attribute.Value{
		"unknown":    {{AttributeID: "nope", Value: "1"}},
		"integer":    {{AttributeID: strength.ID, Value: "1.5"}},
		"enum":       {{AttributeID: mood.ID, Value: "sad"}},
		"flag":       {{AttributeID: armed.ID, Value: "yes"}},
		"both":       {{AttributeID: armed.ID, Name: "armed", Value: "true"}},
		"neither":    {{Value: "x"}},
		"twice":      {{AttributeID: strength.ID, Value: "1"}, {AttributeID: strength.ID, Value: "2"}},
		"name twice": {{Name: "scar", Value: "left cheek"}, {Name: " scar", Value: ""}},
	} {
		_, err := subjects.Create(ctx, g.ID, subject.Input{Name: "Mara", Attributes: vs})
		if !errors.Is(err, attribute.ErrBad) && !errors.Is(err, attribute.ErrNotFound) {
			t.Fatalf("%s: err = %v; want ErrBad or ErrNotFound", name, err)
		}
	}
	mara, err := subjects.Create(ctx, g.ID, subject.Input{Name: "Mara", Actor: true, Attributes: []attribute.Value{
		{AttributeID: strength.ID, Value: " 7 "}, {Name: "scar", Value: "left cheek"}, {AttributeID: armed.ID, Value: "true"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	fen, err := subjects.Create(ctx, g.ID, subject.Input{Name: "Fen", Actor: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := subjects.Update(ctx, g.ID, fen.ID, subject.Input{Name: "Fen", Attributes: []attribute.Value{{AttributeID: mood.ID, Value: "angry"}}}); err != nil {
		t.Fatal(err)
	}
	got, err := subjects.List(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got[0].Attributes) != 3 || got[0].Attributes[0].Value != "7" || got[0].Attributes[1].Name != "scar" || got[1].Attributes[0].AttributeID != mood.ID {
		t.Fatalf("subjects = %+v", got)
	}
	l, err := subjects.AddLink(ctx, g.ID, subject.LinkInput{FromID: mara.ID, ToID: fen.ID, Relation: "trusts", Attributes: []attribute.Value{{AttributeID: strength.ID, Value: "2"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := subjects.UpdateLink(ctx, g.ID, l.ID, subject.LinkInput{FromID: mara.ID, ToID: fen.ID, Relation: "trusts", Attributes: []attribute.Value{{AttributeID: mood.ID, Value: "sad"}}}); !errors.Is(err, attribute.ErrBad) {
		t.Fatalf("link with bad value: err = %v", err)
	}
	if links, err := subjects.Links(ctx, g.ID); err != nil || len(links) != 1 || len(links[0].Attributes) != 1 || links[0].Attributes[0].Value != "2" {
		t.Fatalf("links = %+v, %v", links, err)
	}

	// A declaration cannot change under values that would not fit, nor go while carried.
	err = attrs.Update(ctx, g.ID, strength.ID, attribute.Input{Name: "Strength", Kind: attribute.Flag})
	if !errors.Is(err, attribute.ErrInUse) || !strings.Contains(err.Error(), "Mara = 7") || !strings.Contains(err.Error(), "Mara trusts Fen = 2") {
		t.Fatalf("update under values: err = %v", err)
	}
	if err := attrs.Update(ctx, g.ID, strength.ID, attribute.Input{Name: "Might", Description: "Raw power.", Kind: attribute.Float}); err != nil {
		t.Fatal(err)
	}
	if err := attrs.Update(ctx, g.ID, "nope", attribute.Input{Name: "x", Kind: attribute.Flag}); !errors.Is(err, attribute.ErrNotFound) {
		t.Fatalf("update unknown: err = %v", err)
	}
	if err := attrs.Delete(ctx, g.ID, mood.ID); !errors.Is(err, attribute.ErrInUse) || !strings.Contains(err.Error(), "Fen") {
		t.Fatalf("delete carried: err = %v", err)
	}
	if err := subjects.DeleteLink(ctx, g.ID, l.ID); err != nil {
		t.Fatal(err)
	}
	if err := subjects.Delete(ctx, g.ID, fen.ID); err != nil {
		t.Fatal(err)
	}
	if err := attrs.Delete(ctx, g.ID, mood.ID); err != nil {
		t.Fatalf("delete after its carrier went: %v", err)
	}
	if err := attrs.Delete(ctx, g.ID, "nope"); !errors.Is(err, attribute.ErrNotFound) {
		t.Fatalf("delete unknown: err = %v", err)
	}
}

func TestDescribe(t *testing.T) {
	attrs := []attribute.Attribute{{ID: "a", Name: "Armed", Kind: attribute.Flag}, {ID: "s", Name: "Strength", Kind: attribute.Integer}}
	for _, tc := range []struct {
		v    attribute.Value
		want string
	}{
		{attribute.Value{AttributeID: "a", Value: "true"}, "Armed"},
		{attribute.Value{AttributeID: "a", Value: "false"}, "not Armed"},
		{attribute.Value{AttributeID: "s", Value: "7"}, "Strength: 7"},
		{attribute.Value{Name: "scar", Value: "left cheek"}, "scar: left cheek"},
		{attribute.Value{Name: "scarred"}, "scarred"},
	} {
		if got := attribute.Describe(tc.v, attrs); got != tc.want {
			t.Errorf("Describe(%+v) = %q; want %q", tc.v, got, tc.want)
		}
	}
}
