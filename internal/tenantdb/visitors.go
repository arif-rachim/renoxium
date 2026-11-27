package tenantdb

import (
	"database/sql"
	"os"
	"time"
)

// Visitor is a visitor account carried from one preview database to the
// next, with its sign-in sessions that have not expired.
type Visitor struct {
	ID                               int64
	Email, Phone, Name, PasswordHash sql.NullString
	VerifiedAt                       sql.NullString
	CreatedAt                        string
	Sessions                         []Session
}

// Session is one sign-in session of a Visitor.
type Session struct {
	Hash      string
	ExpiresAt int64
}

// SaveVisitors reads the visitor accounts of the database at path, with
// their live sessions, skipping accounts for which skip is true. A missing
// file has none.
func SaveVisitors(path string, skip func(email string) bool) ([]Visitor, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}
	db, err := openAdmin(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, email, phone, name, password_hash, verified_at, created_at FROM rx_users ORDER BY id`)
	if err != nil {
		return nil, nil // no visitor table yet
	}
	var out []Visitor
	for rows.Next() {
		var v Visitor
		if err := rows.Scan(&v.ID, &v.Email, &v.Phone, &v.Name, &v.PasswordHash, &v.VerifiedAt, &v.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if skip != nil && skip(v.Email.String) {
			continue
		}
		out = append(out, v)
	}
	rows.Close()
	for i := range out {
		srows, err := db.Query(`SELECT hash, expires_at FROM rx_sessions WHERE user_id = ? AND expires_at > ?`, out[i].ID, time.Now().Unix())
		if err != nil {
			continue
		}
		for srows.Next() {
			var s Session
			if srows.Scan(&s.Hash, &s.ExpiresAt) == nil {
				out[i].Sessions = append(out[i].Sessions, s)
			}
		}
		srows.Close()
	}
	return out, nil
}

// RestoreVisitors puts visitors saved with SaveVisitors into the database at
// path, migrated already. A visitor whose email or phone is there already
// (copied from live) keeps that account, and gets the sessions; otherwise
// the visitor keeps its id when that is free, so rows that point at it
// still do.
func RestoreVisitors(path string, visitors []Visitor) error {
	if len(visitors) == 0 {
		return nil
	}
	db, err := openAdmin(path)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, v := range visitors {
		var id int64
		err := tx.QueryRow(`SELECT id FROM rx_users WHERE (email IS NOT NULL AND email = ?) OR (phone IS NOT NULL AND phone = ?)`, v.Email, v.Phone).Scan(&id)
		if err == sql.ErrNoRows {
			var taken int
			tx.QueryRow(`SELECT count(*) FROM rx_users WHERE id = ?`, v.ID).Scan(&taken)
			var res sql.Result
			if taken == 0 {
				res, err = tx.Exec(`INSERT INTO rx_users (id, email, phone, name, password_hash, verified_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
					v.ID, v.Email, v.Phone, v.Name.String, v.PasswordHash, v.VerifiedAt, v.CreatedAt)
			} else {
				res, err = tx.Exec(`INSERT INTO rx_users (email, phone, name, password_hash, verified_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
					v.Email, v.Phone, v.Name.String, v.PasswordHash, v.VerifiedAt, v.CreatedAt)
			}
			if err != nil {
				return err
			}
			id, _ = res.LastInsertId()
		} else if err != nil {
			return err
		}
		for _, s := range v.Sessions {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO rx_sessions (hash, user_id, expires_at) VALUES (?, ?, ?)`, s.Hash, id, s.ExpiresAt); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
