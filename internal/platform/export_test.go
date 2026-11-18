package platform

import (
	"fmt"
	"testing"
)

// SeedFiles records n placeholder files for an app in one transaction, so a
// test can reach the file limit without n uploads that each sync the disk.
func SeedFiles(t *testing.T, p *Platform, app string, n int) {
	t.Helper()
	tx, err := p.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < n; i++ {
		hash := fmt.Sprintf("seed%060d", i)
		if _, err := tx.Exec(`INSERT INTO blobs (hash, size, content_type, created_at) VALUES (?, 1, 'image/png', ?)`, hash, now()); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO app_files (app, hash, created_at) VALUES (?, ?, ?)`, app, hash, now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
