package accounts

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"time"
)

// Connections are the agents (ChatGPT, Claude, ...) an owner allowed to
// work on their account: each holds tokens until it is disconnected, the
// password changes or its refresh token expires. grants keeps when each
// was connected and last renewed its tokens; the tokens themselves rotate
// and say neither.

const grantsTable = `CREATE TABLE IF NOT EXISTS grants (user_id TEXT NOT NULL, client_id TEXT NOT NULL,
	connected_at INTEGER NOT NULL, last_used_at INTEGER NOT NULL, PRIMARY KEY (user_id, client_id))`

// Connection is one agent connected to an account.
type Connection struct {
	ClientID    string
	Name        string    // the name the agent registered with; anyone can choose it
	Host        string    // where its sign-in returned to, e.g. claude.ai
	ConnectedAt time.Time // zero for agents connected before this was recorded
	LastUsedAt  time.Time
}

func (s *Store) noteGrant(tx *sql.Tx, userID, clientID string, connected bool) error {
	now := s.now().Unix()
	if connected {
		_, err := tx.Exec(`INSERT INTO grants (user_id, client_id, connected_at, last_used_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (user_id, client_id) DO UPDATE SET connected_at = excluded.connected_at, last_used_at = excluded.last_used_at`,
			userID, clientID, now, now)
		return err
	}
	_, err := tx.Exec(`UPDATE grants SET last_used_at = ? WHERE user_id = ? AND client_id = ?`, now, userID, clientID)
	return err
}

// Connections lists the agents that can still act for userID, most
// recently used first.
func (s *Store) Connections(userID string) ([]Connection, error) {
	rows, err := s.db.Query(`SELECT c.id, c.name, c.redirect_uris, coalesce(g.connected_at, 0), coalesce(g.last_used_at, 0)
		FROM (SELECT DISTINCT client_id FROM tokens WHERE user_id = ? AND refresh_expires_at > ?) t
		JOIN clients c ON c.id = t.client_id
		LEFT JOIN grants g ON g.user_id = ? AND g.client_id = t.client_id
		ORDER BY coalesce(g.last_used_at, 0) DESC, c.name`, userID, s.now().Unix(), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connection
	for rows.Next() {
		var c Connection
		var uris string
		var connected, used int64
		if err := rows.Scan(&c.ClientID, &c.Name, &uris, &connected, &used); err != nil {
			return nil, err
		}
		var list []string
		if json.Unmarshal([]byte(uris), &list) == nil && len(list) > 0 {
			if u, err := url.Parse(list[0]); err == nil {
				c.Host = u.Host
			}
		}
		if connected > 0 {
			c.ConnectedAt = time.Unix(connected, 0)
		}
		if used > 0 {
			c.LastUsedAt = time.Unix(used, 0)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Disconnect ends one agent's access to userID's account at once: its
// tokens and unused sign-in codes go. It reports whether there was any.
func (s *Store) Disconnect(userID, clientID string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM tokens WHERE user_id = ? AND client_id = ?`, userID, clientID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	for _, stmt := range []string{`DELETE FROM codes WHERE user_id = ? AND client_id = ?`, `DELETE FROM grants WHERE user_id = ? AND client_id = ?`} {
		if _, err := tx.Exec(stmt, userID, clientID); err != nil {
			return false, err
		}
	}
	return n > 0, tx.Commit()
}
