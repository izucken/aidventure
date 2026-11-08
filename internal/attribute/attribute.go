// Package attribute holds the qualities subjects and links carry beyond
// their prose: a value under a name. A value is structured when the game
// declares the attribute, with a kind that says what fits, and freeform when
// the author simply names it on the spot.
package attribute

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"aidventure/internal/game"
)

var (
	ErrNotFound = errors.New("attribute not found")
	ErrBad      = errors.New("invalid attribute")
	// ErrInUse means values carry the attribute and the change would strand them.
	ErrInUse = errors.New("attribute is in use")
)

// Kind is what a declared attribute's value may be.
type Kind string

const (
	Flag    Kind = "flag"
	Integer Kind = "integer"
	Float   Kind = "float"
	Enum    Kind = "enum"
)

var kinds = []Kind{Flag, Integer, Float, Enum}

// Attribute is a quality the game declares for its subjects and links to carry.
type Attribute struct {
	ID          string `json:"id"`
	GameID      string `json:"gameId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        Kind   `json:"kind"`
	// Options are what an enum may be; empty for every other kind.
	Options   []string  `json:"options"`
	CreatedAt time.Time `json:"createdAt"`
}

// Input is what an author sets on a declared attribute.
type Input struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Kind        Kind     `json:"kind"`
	Options     []string `json:"options"`
}

func (in *Input) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" {
		return fmt.Errorf("%w: name is empty", ErrBad)
	}
	if !slices.Contains(kinds, in.Kind) {
		return fmt.Errorf("%w: kind %q is none of %v", ErrBad, in.Kind, kinds)
	}
	if in.Kind != Enum {
		in.Options = []string{}
		return nil
	}
	options := make([]string, 0, len(in.Options))
	for _, o := range in.Options {
		o = strings.TrimSpace(o)
		switch {
		case o == "":
			return fmt.Errorf("%w: enum %s has an empty option", ErrBad, in.Name)
		case slices.Contains(options, o):
			return fmt.Errorf("%w: enum %s lists option %q twice", ErrBad, in.Name, o)
		}
		options = append(options, o)
	}
	if len(options) == 0 {
		return fmt.Errorf("%w: enum %s has no options", ErrBad, in.Name)
	}
	in.Options = options
	return nil
}

// Querier is the slice of *sql.DB and *sql.Tx this package reads through.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, gameID string, in Input) (Attribute, error) {
	if err := in.validate(); err != nil {
		return Attribute{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Attribute{}, fmt.Errorf("create attribute: %w", err)
	}
	a := Attribute{
		ID: id.String(), GameID: gameID, Name: in.Name, Description: in.Description, Kind: in.Kind, Options: in.Options,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	options, err := json.Marshal(a.Options)
	if err != nil {
		return Attribute{}, fmt.Errorf("create attribute: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Attribute{}, fmt.Errorf("create attribute: %w", err)
	}
	defer tx.Rollback()
	if err := game.Require(ctx, tx, gameID); err != nil {
		return Attribute{}, err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO attributes (id, game_id, name, description, kind, options, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.GameID, a.Name, a.Description, a.Kind, string(options), a.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return Attribute{}, fmt.Errorf("create attribute: %w", err)
	}
	return a, tx.Commit()
}

// List returns the game's declared attributes in the order they were made.
func (s *Store) List(ctx context.Context, gameID string) ([]Attribute, error) {
	if err := game.Require(ctx, s.db, gameID); err != nil {
		return nil, err
	}
	return Read(ctx, s.db, gameID)
}

// Read is the game's declared attributes, for whoever renders a prompt or checks a value.
func Read(ctx context.Context, q Querier, gameID string) ([]Attribute, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, game_id, name, description, kind, options, created_at FROM attributes WHERE game_id = ? ORDER BY id`, gameID)
	if err != nil {
		return nil, fmt.Errorf("list attributes: %w", err)
	}
	defer rows.Close()
	attrs := []Attribute{}
	for rows.Next() {
		var a Attribute
		var options, created string
		if err := rows.Scan(&a.ID, &a.GameID, &a.Name, &a.Description, &a.Kind, &options, &created); err != nil {
			return nil, fmt.Errorf("list attributes: %w", err)
		}
		if err := json.Unmarshal([]byte(options), &a.Options); err != nil {
			return nil, fmt.Errorf("list attributes: options of %s: %w", a.Name, err)
		}
		if a.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			return nil, fmt.Errorf("list attributes: %w", err)
		}
		attrs = append(attrs, a)
	}
	return attrs, rows.Err()
}

// Update replaces a declaration. Values already carried under it must still
// fit the new kind and options; otherwise nothing changes and ErrInUse says
// which values would not.
func (s *Store) Update(ctx context.Context, gameID, id string, in Input) error {
	if err := in.validate(); err != nil {
		return err
	}
	options, err := json.Marshal(in.Options)
	if err != nil {
		return fmt.Errorf("update attribute: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("update attribute: %w", err)
	}
	defer tx.Rollback()
	carried, err := carriers(ctx, tx, id)
	if err != nil {
		return err
	}
	var misfits []string
	next := Attribute{Name: in.Name, Kind: in.Kind, Options: in.Options}
	for _, c := range carried {
		if err := next.check(c.value); err != nil {
			misfits = append(misfits, c.owner+" = "+c.value)
		}
	}
	if len(misfits) > 0 {
		return fmt.Errorf("%w: %s as %s would not fit %s", ErrInUse, in.Name, in.Kind, strings.Join(misfits, ", "))
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE attributes SET name = ?, description = ?, kind = ?, options = ? WHERE id = ? AND game_id = ?`,
		in.Name, in.Description, in.Kind, string(options), id, gameID)
	if err != nil {
		return fmt.Errorf("update attribute: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("update attribute: %w", err)
	} else if n == 0 {
		return fmt.Errorf("%w: attribute %s in game %s", ErrNotFound, id, gameID)
	}
	return tx.Commit()
}

// Delete removes a declaration nothing carries; ErrInUse says who does otherwise.
func (s *Store) Delete(ctx context.Context, gameID, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete attribute: %w", err)
	}
	defer tx.Rollback()
	carried, err := carriers(ctx, tx, id)
	if err != nil {
		return err
	}
	if len(carried) > 0 {
		owners := make([]string, 0, len(carried))
		for _, c := range carried {
			owners = append(owners, c.owner)
		}
		return fmt.Errorf("%w: carried by %s", ErrInUse, strings.Join(owners, ", "))
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM attributes WHERE id = ? AND game_id = ?`, id, gameID)
	if err != nil {
		return fmt.Errorf("delete attribute: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("delete attribute: %w", err)
	} else if n == 0 {
		return fmt.Errorf("%w: attribute %s in game %s", ErrNotFound, id, gameID)
	}
	return tx.Commit()
}

// carrier is one value under a declaration, with its owner named the way the
// author knows it, for errors.
type carrier struct {
	owner, value string
}

func carriers(ctx context.Context, q Querier, attributeID string) ([]carrier, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT s.name, v.value FROM attribute_values v JOIN subjects s ON s.id = v.subject_id WHERE v.attribute_id = ?
		 UNION ALL
		 SELECT f.name || ' ' || l.relation || ' ' || t.name, v.value FROM attribute_values v
		   JOIN links l ON l.id = v.link_id JOIN subjects f ON f.id = l.from_id JOIN subjects t ON t.id = l.to_id
		 WHERE v.attribute_id = ?`, attributeID, attributeID)
	if err != nil {
		return nil, fmt.Errorf("find carriers of attribute %s: %w", attributeID, err)
	}
	defer rows.Close()
	var out []carrier
	for rows.Next() {
		var c carrier
		if err := rows.Scan(&c.owner, &c.value); err != nil {
			return nil, fmt.Errorf("find carriers of attribute %s: %w", attributeID, err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
