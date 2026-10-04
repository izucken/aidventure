package store

import (
	"path/filepath"
	"testing"
)

func TestOpenAppliesMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	// Open twice: the second run must find nothing left to apply.
	for i := range 2 {
		db, err := Open(t.Context(), path)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		var n int
		if err := db.QueryRow("SELECT count(*) FROM settings").Scan(&n); err != nil {
			t.Fatalf("open #%d: settings table missing: %v", i+1, err)
		}
		db.Close()
	}
}
