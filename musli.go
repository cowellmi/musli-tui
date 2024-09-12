package musli

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
	_ "github.com/mattn/go-sqlite3"
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

func OpenDB(path string) (*sql.DB, error) {
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

func CloseDB(db *sql.DB) error {
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

func isAudioFile(path string) bool {
	ext := filepath.Ext(path)
	switch strings.ToUpper(ext) {
	case
		".MP3", ".M4A", ".M4B", ".M4P", ".ALAC", ".FLAC", ".OGG", ".DSF":
		return true
	}
	return false
}

func FindAudioFilePaths(dir string) ([]string, error) {
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

func readMetadata(path string) (*Album, *Track, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0444)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return nil, nil, err
	}

	a := Album{
		AlbumArtist: m.AlbumArtist(),
		Name:        m.Album(),
		Year:        m.Year(),
	}
	if m.Year() == 0 {
		a.Year = readAltYearMetadata(m)
	}

	disc, _ := m.Disc()
	trackNumber, _ := m.Track()
	t := Track{
		Disc:        disc,
		Path:        path,
		TrackNumber: trackNumber,
	}

	return &a, &t, nil
}

func AddPath(db *sql.DB, path string) error {
	trackID, err := findTrackID(path, db)
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

	albumID, err := findAlbumID(a, db)
	if err != nil {
		return err
	}
	if albumID == -1 { // album doesn't exist
		albumID, err = insertAlbum(a, db)
		if err != nil {
			return err
		}
	}
	t.AlbumID = albumID

	_, err = insertTrack(db, t)
	if err != nil {
		return err
	}
	return nil
}

func findAlbumID(a *Album, db *sql.DB) (int64, error) {
	query := `SELECT id FROM albums
			WHERE album_artist = ? AND name = ? AND year = ?;`
	row := db.QueryRow(query, a.AlbumArtist, a.Name, a.Year)
	var albumID int64
	err := row.Scan(&albumID)
	if err == sql.ErrNoRows {
		return -1, nil
	}
	return albumID, err
}

func findTrackID(path string, db *sql.DB) (int64, error) {
	query := `SELECT id FROM tracks WHERE path = ?;`
	row := db.QueryRow(query, path)
	var trackID int64
	err := row.Scan(&trackID)
	if err == sql.ErrNoRows {
		return -1, nil
	}
	return trackID, err
}

func insertAlbum(a *Album, db *sql.DB) (int64, error) {
	res, err := db.Exec(`INSERT INTO albums(album_artist,name,year)
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

func DeleteTrack(db *sql.DB, path string) error {
	_, err := db.Exec(`DELETE FROM tracks WHERE path = ?`, path)
	return err
}

func fetchAlbumIDs(db *sql.DB) ([]int64, error) {
	query := `SELECT id FROM albums`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}

	albumIDs, err := parseRowsToAlbumIDs(rows)
	if err != nil {
		return nil, err
	}

	return albumIDs, nil
}

func RemoveEmptyAlbums(db *sql.DB) error {
	tx, err := db.Begin()
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

func parseRowsToAlbums(rows *sql.Rows) ([]Album, error) {
	var albums []Album
	for rows.Next() {
		var a Album
		err := rows.Scan(&a.ID, &a.AlbumArtist, &a.Name, &a.Year)
		if err != nil {
			return nil, err
		}
		albums = append(albums, a)
	}
	return albums, nil
}

func GetRandomAlbums(db *sql.DB) ([]Album, error) {
	rows, err := db.Query("SELECT * FROM albums ORDER BY RANDOM();")
	if err != nil {
		return nil, err
	}

	albums, err := parseRowsToAlbums(rows)
	if err != nil {
		return nil, err
	}

	return albums, nil
}

func GetOneRandomAlbum(db *sql.DB) (*Album, error) {
	row := db.QueryRow("SELECT * FROM albums ORDER BY RANDOM() LIMIT 1;")

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

func sqliteOrder(asc bool) string {
	if asc {
		return "ASC"
	}
	return "DESC"
}

func GetAlbumsByArtist(db *sql.DB, asc bool) ([]Album, error) {
	rows, err := db.Query(`SELECT * FROM albums
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

func GetAlbumsByYear(db *sql.DB, asc bool) ([]Album, error) {
	rows, err := db.Query(`SELECT * FROM albums
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

func SearchAlbums(db *sql.DB, query string) ([]Album, error) {
	a := "%" + query + "%"
	rows, err := db.Query(`SELECT * FROM albums WHERE
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

func readAltYearMetadata(m tag.Metadata) int {
	// https://eyed3.readthedocs.io/en/latest/compliance.html
	r := m.Raw()
	tdor := (r["TDOR"]) // ID3 v2.4 orig release date
	if tdorStr, ok := tdor.(string); ok {
		yearStr := strings.Split(tdorStr, "-")[0]
		year, err := strconv.Atoi(yearStr)
		if err == nil {
			return year
		}
	}
	tdrl := (r["TDRL"]) // ID3 v2.4 release date
	if tdrlStr, ok := tdrl.(string); ok {
		fmt.Println("TDRL", tdrlStr)
	}
	xdor := (r["XDOR"]) // ID3 v2.3 orig release year
	if xdorStr, ok := xdor.(string); ok {
		fmt.Println("XDOR", xdorStr)
	}
	tory := (r["TORY"]) // ID3 v2.3 orig release year
	if toryStr, ok := tory.(string); ok {
		fmt.Println("TORY", toryStr)
	}
	return 0
}

func insertTrack(db *sql.DB, t *Track) (int64, error) {
	res, err := db.Exec(`INSERT INTO tracks(album_id,disc,path,track_number)
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

func AlbumTrackPaths(db *sql.DB, albumID int64) ([]string, error) {
	query := `SELECT path FROM tracks
			WHERE album_id = ?
			ORDER BY track_number ASC, disc ASC;`
	rows, err := db.Query(query, albumID)
	if err != nil {
		return nil, err
	}

	paths, err := parseRowsToTrackPaths(rows)
	if err != nil {
		return nil, err
	}

	return paths, nil
}

func AllTrackPaths(db *sql.DB) ([]string, error) {
	query := `SELECT path FROM tracks`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}

	paths, err := parseRowsToTrackPaths(rows)
	if err != nil {
		return nil, err
	}

	return paths, nil
}

func parseRowsToAlbumIDs(rows *sql.Rows) ([]int64, error) {
	var albumIDs []int64
	for rows.Next() {
		var a int64
		err := rows.Scan(&a)
		if err != nil {
			return nil, err
		}
		albumIDs = append(albumIDs, a)
	}
	return albumIDs, nil
}

func parseRowsToTrackPaths(rows *sql.Rows) ([]string, error) {
	var paths []string
	for rows.Next() {
		var p string
		err := rows.Scan(&p)
		if err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}
