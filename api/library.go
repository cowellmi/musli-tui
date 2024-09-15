package api

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Album struct {
	ID          int64
	AlbumArtist string
	Name        string
	Year        int
}

type Track struct {
	AlbumID     int64
	Disc        int
	TrackNumber int
	Path        string
}

type Library struct {
	db *sql.DB
}

func NewLibrary() (Library, error) {
	var lib Library

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return lib, err
	}

	dbPath := filepath.Join(cacheDir, "musli", "library.db")

	err = os.MkdirAll(filepath.Dir(dbPath), os.ModePerm)
	if err != nil {
		return lib, err
	}

	db, err := openDB(dbPath)
	if err != nil {
		return lib, err
	}

	lib.db = db

	return lib, nil
}

func (lib Library) Close() error {
	return closeDB(lib.db)
}

func (lib Library) FindAudioFilePaths(dir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, di fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !di.IsDir() && isAudioFile(path) {
			paths = append(paths, path)
		}

		return nil
	})
	return paths, err
}

// Add track (and album, if not present) to library
func (lib Library) AddPathToLibrary(path string) error {
	trackID, err := lib.findTrackID(path)
	if err != nil {
		return err
	}
	if trackID != -1 { // path already in db
		return nil
	}

	a, t, err := readMetadata(path)
	if err != nil {
		return err
	}

	albumID, err := lib.findAlbumID(a)
	if err != nil {
		return err
	}
	if albumID == -1 { // album doesn't exist
		albumID, err = lib.insertAlbum(a)
		if err != nil {
			return err
		}
	}
	t.AlbumID = albumID

	_, err = lib.insertTrack(t)
	if err != nil {
		return err
	}
	return nil
}

func (lib Library) findTrackID(path string) (int64, error) {
	query := `SELECT id FROM tracks WHERE path = ?;`
	row := lib.db.QueryRow(query, path)
	var trackID int64
	err := row.Scan(&trackID)
	if err == sql.ErrNoRows {
		return -1, nil
	}
	return trackID, err
}

func (lib Library) findAlbumID(a *Album) (int64, error) {
	query := `SELECT id FROM albums
			WHERE album_artist = ? AND name = ? AND year = ?;`
	row := lib.db.QueryRow(query, a.AlbumArtist, a.Name, a.Year)
	var albumID int64
	err := row.Scan(&albumID)
	if err == sql.ErrNoRows {
		return -1, nil
	}
	return albumID, err
}

func (lib Library) insertAlbum(a *Album) (int64, error) {
	res, err := lib.db.Exec(`INSERT INTO albums(album_artist,name,year)
						VALUES(?,?,?);`, a.AlbumArtist, a.Name, a.Year)
	if err != nil {
		return -1, err
	}
	albumID, err := res.LastInsertId()
	if err != nil {
		return -1, err
	}
	return albumID, nil
}

func (lib Library) insertTrack(t *Track) (int64, error) {
	res, err := lib.db.Exec(`INSERT INTO tracks(album_id,disc,path,track_number)
						VALUES(?,?,?,?);`, t.AlbumID, t.Disc, t.Path, t.TrackNumber)
	if err != nil {
		return -1, err
	}
	trackID, err := res.LastInsertId()
	if err != nil {
		return -1, err
	}
	return trackID, nil
}

func (lib Library) DeleteTrack(path string) error {
	_, err := lib.db.Exec(`DELETE FROM tracks WHERE path = ?`, path)
	return err
}

// Delete all albums with no tracks
func (lib Library) RemoveEmptyAlbums() error {
	tx, err := lib.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM albums
					WHERE id NOT IN (
						SELECT DISTINCT album_id
						FROM tracks
					);`)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (lib Library) GetRandomAlbums() ([]Album, error) {
	rows, err := lib.db.Query("SELECT * FROM albums ORDER BY RANDOM();")
	if err != nil {
		return nil, err
	}

	albums, err := parseRowsToAlbums(rows)
	if err != nil {
		return nil, err
	}

	return albums, nil
}

func (lib Library) GetOneRandomAlbum() (*Album, error) {
	row := lib.db.QueryRow("SELECT * FROM albums ORDER BY RANDOM() LIMIT 1;")

	var a Album
	err := row.Scan(&a.ID, &a.AlbumArtist, &a.Name, &a.Year)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No albums found
		}
		return nil, err
	}

	return &a, nil
}

func (lib Library) GetAlbumsByArtist(asc bool) ([]Album, error) {
	rows, err := lib.db.Query(`SELECT * FROM albums
						ORDER BY album_artist ` + sqliteOrder(asc))
	if err != nil {
		return nil, err
	}

	albums, err := parseRowsToAlbums(rows)
	if err != nil {
		return nil, err
	}

	return albums, nil
}

func (lib Library) GetAlbumsByYear(asc bool) ([]Album, error) {
	rows, err := lib.db.Query(`SELECT * FROM albums
						ORDER BY year ` + sqliteOrder(asc))
	if err != nil {
		return nil, err
	}

	albums, err := parseRowsToAlbums(rows)
	if err != nil {
		return nil, err
	}

	return albums, nil
}

func (lib Library) SearchAlbums(query string) ([]Album, error) {
	a := "%" + query + "%"
	rows, err := lib.db.Query(`SELECT * FROM albums WHERE
						name LIKE ? OR album_artist LIKE ?
						ORDER BY album_artist ASC, name ASC;`, a, a)
	if err != nil {
		return nil, err
	}

	albums, err := parseRowsToAlbums(rows)
	if err != nil {
		return nil, err
	}

	return albums, nil
}

func (lib Library) AlbumTrackPaths(albumID int64) ([]string, error) {
	query := `SELECT path FROM tracks
			WHERE album_id = ?
			ORDER BY track_number ASC, disc ASC;`
	rows, err := lib.db.Query(query, albumID)
	if err != nil {
		return nil, err
	}

	paths, err := parseRowsToTrackPaths(rows)
	if err != nil {
		return nil, err
	}

	return paths, nil
}

func (lib Library) AllTrackPaths() ([]string, error) {
	query := `SELECT path FROM tracks`
	rows, err := lib.db.Query(query)
	if err != nil {
		return nil, err
	}

	paths, err := parseRowsToTrackPaths(rows)
	if err != nil {
		return nil, err
	}

	return paths, nil
}

func (lib Library) PlayAlbum(albumID int64, execCmd string, debug bool) error {
	paths, err := lib.AlbumTrackPaths(albumID)
	if err != nil {
		return err
	}

	c := strings.Split(execCmd, " ")
	args := append(c[1:], paths...)
	cmd := exec.Command(c[0], args...)

	if debug {
		ct := time.Now()
		ft := ct.Format("2006-01-02_15-04-05")
		name := fmt.Sprintf("musli_%s.txt", ft)

		file, err := os.Create(name)
		if err != nil {
			return err
		}
		defer file.Close()

		cmd.Stdout = file
		cmd.Stderr = file
	}

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}
