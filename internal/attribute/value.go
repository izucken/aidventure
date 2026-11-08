package attribute

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// Value is an attribute as a subject or a link carries it. Under a declared
// attribute it refers to the declaration; freeform, it carries its own name.
// The value is text whatever the kind; the kind says what text fits.
type Value struct {
	AttributeID string `json:"attributeId,omitempty"`
	Name        string `json:"name,omitempty"`
	Value       string `json:"value"`
}

// check fails with ErrBad unless the text fits the attribute's kind.
func (a Attribute) check(v string) error {
	switch a.Kind {
	case Flag:
		if v != "true" && v != "false" {
			return fmt.Errorf("%w: flag %s is %q, not true or false", ErrBad, a.Name, v)
		}
	case Integer:
		if _, err := strconv.ParseInt(v, 10, 64); err != nil {
			return fmt.Errorf("%w: integer %s is %q", ErrBad, a.Name, v)
		}
	case Float:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("%w: float %s is %q", ErrBad, a.Name, v)
		}
	case Enum:
		if !slices.Contains(a.Options, v) {
			return fmt.Errorf("%w: enum %s is %q, none of %v", ErrBad, a.Name, v, a.Options)
		}
	}
	return nil
}

// owner is which column a value hangs off: the one for subjects or the one for links.
type owner string

const (
	ofSubject owner = "subject_id"
	ofLink    owner = "link_id"
)

// SubjectValues are the values every subject of the game carries, by subject id.
func SubjectValues(ctx context.Context, q Querier, gameID string) (map[string][]Value, error) {
	return values(ctx, q, ofSubject, `JOIN subjects o ON o.id = v.subject_id WHERE o.game_id = ?`, gameID)
}

// LinkValues are the values every link of the game carries, by link id.
func LinkValues(ctx context.Context, q Querier, gameID string) (map[string][]Value, error) {
	return values(ctx, q, ofLink, `JOIN links o ON o.id = v.link_id WHERE o.game_id = ?`, gameID)
}

func values(ctx context.Context, q Querier, of owner, scope, gameID string) (map[string][]Value, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT v.`+string(of)+`, v.attribute_id, v.name, v.value FROM attribute_values v `+scope+` ORDER BY v.`+string(of)+`, v.seq`, gameID)
	if err != nil {
		return nil, fmt.Errorf("list attribute values: %w", err)
	}
	defer rows.Close()
	out := map[string][]Value{}
	for rows.Next() {
		var id string
		var attr, name sql.NullString
		var v Value
		if err := rows.Scan(&id, &attr, &name, &v.Value); err != nil {
			return nil, fmt.Errorf("list attribute values: %w", err)
		}
		v.AttributeID, v.Name = attr.String, name.String
		out[id] = append(out[id], v)
	}
	return out, rows.Err()
}

// SetSubjectValues replaces what a subject carries, inside the caller's transaction.
func SetSubjectValues(ctx context.Context, tx *sql.Tx, gameID, subjectID string, vs []Value) error {
	return set(ctx, tx, ofSubject, gameID, subjectID, vs)
}

// SetLinkValues replaces what a link carries, inside the caller's transaction.
func SetLinkValues(ctx context.Context, tx *sql.Tx, gameID, linkID string, vs []Value) error {
	return set(ctx, tx, ofLink, gameID, linkID, vs)
}

func set(ctx context.Context, tx *sql.Tx, of owner, gameID, ownerID string, vs []Value) error {
	declared, err := Read(ctx, tx, gameID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM attribute_values WHERE `+string(of)+` = ?`, ownerID); err != nil {
		return fmt.Errorf("set attribute values: %w", err)
	}
	seen := map[string]bool{}
	for i, v := range vs {
		v.Name, v.Value = strings.TrimSpace(v.Name), strings.TrimSpace(v.Value)
		var attr, name any
		var label string
		switch {
		case v.AttributeID != "" && v.Name != "":
			return fmt.Errorf("%w: value %q is both declared and freeform", ErrBad, v.Name)
		case v.AttributeID != "":
			at := slices.IndexFunc(declared, func(a Attribute) bool { return a.ID == v.AttributeID })
			if at < 0 {
				return fmt.Errorf("%w: attribute %s in game %s", ErrNotFound, v.AttributeID, gameID)
			}
			if err := declared[at].check(v.Value); err != nil {
				return err
			}
			attr, label = v.AttributeID, declared[at].Name
		case v.Name != "":
			name, label = v.Name, v.Name
		default:
			return fmt.Errorf("%w: a value with neither an attribute nor a name", ErrBad)
		}
		if seen[label] {
			return fmt.Errorf("%w: %s is set twice", ErrBad, label)
		}
		seen[label] = true
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO attribute_values (`+string(of)+`, attribute_id, name, value, seq) VALUES (?, ?, ?, ?, ?)`,
			ownerID, attr, name, v.Value, i); err != nil {
			return fmt.Errorf("set attribute values: %w", err)
		}
	}
	return nil
}

// Describe writes a value as prose reads it: a flag by its name alone, or
// negated; anything else as name and value. Declared names come from attrs.
func Describe(v Value, attrs []Attribute) string {
	if v.AttributeID == "" {
		if v.Value == "" {
			return v.Name
		}
		return v.Name + ": " + v.Value
	}
	i := slices.IndexFunc(attrs, func(a Attribute) bool { return a.ID == v.AttributeID })
	if i < 0 {
		return v.Value
	}
	a := attrs[i]
	if a.Kind == Flag {
		if v.Value == "true" {
			return a.Name
		}
		return "not " + a.Name
	}
	return a.Name + ": " + v.Value
}
