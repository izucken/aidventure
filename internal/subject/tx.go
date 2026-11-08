package subject

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"aidventure/internal/attribute"
)

// Insert creates a subject inside a caller's transaction, for operations
// that create a subject together with other rows. The game must exist.
func Insert(ctx context.Context, tx *sql.Tx, gameID string, in Input) (Subject, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Subject{}, fmt.Errorf("create subject: %w", err)
	}
	sub := Subject{
		ID: id.String(), GameID: gameID, Name: in.Name, Description: in.Description,
		Actor: in.Actor, Scene: in.Scene, User: in.User, Tracked: in.Tracked, Attributes: in.Attributes,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if sub.Attributes == nil {
		sub.Attributes = []attribute.Value{}
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO subjects (id, game_id, name, description, actor, scene, user, tracked, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sub.ID, sub.GameID, sub.Name, sub.Description, sub.Actor, sub.Scene, sub.User, sub.Tracked, sub.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return Subject{}, fmt.Errorf("create subject: %w", err)
	}
	if err := attribute.SetSubjectValues(ctx, tx, gameID, sub.ID, sub.Attributes); err != nil {
		return Subject{}, err
	}
	return sub, nil
}
