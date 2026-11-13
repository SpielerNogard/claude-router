package store

import "time"

// An empty list means no result rather than zero models, so the previously stored list is kept.
func (s *Store) SaveModels(provider string, models []string) error {
	if len(models) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM discovered_models WHERE provider = ?`, provider); err != nil {
		return err
	}
	now := time.Now().UTC().Unix()
	for _, m := range models {
		if _, err := tx.Exec(`INSERT INTO discovered_models (provider, model, seen_at) VALUES (?,?,?)`, provider, m, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Models() (map[string][]string, error) {
	rows, err := s.db.Query(`SELECT provider, model FROM discovered_models ORDER BY provider, model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var p, m string
		if err := rows.Scan(&p, &m); err != nil {
			return nil, err
		}
		out[p] = append(out[p], m)
	}
	return out, rows.Err()
}
