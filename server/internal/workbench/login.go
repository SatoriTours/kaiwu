package workbench

import "time"

// SaveLogin persists only hashes, never the access token or browser credential.
// Sessions are independent of board revisions and agent state.
func (s *Store) SaveLogin(idHash, tokenHash string, expires time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM login_sessions WHERE expires<=? OR token_hash<>?", time.Now().Unix(), tokenHash); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRow("SELECT count(*) FROM login_sessions").Scan(&count); err != nil {
		return err
	}
	if count >= 100 {
		return Conflict("登录设备已达上限，请先退出其他设备")
	}
	if _, err = tx.Exec("INSERT INTO login_sessions VALUES (?,?,?)", idHash, tokenHash, expires.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ValidLogin(idHash, tokenHash string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	var valid bool
	err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM login_sessions WHERE id_hash=? AND token_hash=? AND expires>?)", idHash, tokenHash, time.Now().Unix()).Scan(&valid)
	return err == nil && valid
}

func (s *Store) RevokeLogin(idHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM login_sessions WHERE id_hash=?", idHash)
	return err
}
