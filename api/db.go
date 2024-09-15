package api

import (
	"database/sql"
)

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`PRAGMA journal_mode = wal;
					PRAGMA synchronous = normal;
					PRAGMA foreign_keys = on;`)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS albums(
						id integer PRIMARY KEY,
						album_artist TEXT,
						name TEXT,
						year INTEGER
					);`)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS tracks(
						id integer PRIMARY KEY,
						album_id INTEGER REFERENCES albums(id),
						disc INTEGER,
						path TEXT,
						track_number INTEGER
					);`)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func closeDB(db *sql.DB) error {
	if db == nil {
		return nil
	}

	_, err := db.Exec(`PRAGMA analysis_limit=400;
					PRAGMA optimize;`)
	if err != nil {
		return err
	}

	err = db.Close()
	if err != nil {
		return err
	}

	return nil
}
