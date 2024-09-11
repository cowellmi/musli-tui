package musli

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
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
						album_id INTEGER,
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

func GetMusicDirPaths(dir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, di fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if isValidFileType(path) {
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

func AddPathToLibrary(path string, db *sql.DB) error {
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

	_, err = insertTrack(t, db)
	if err != nil {
		return err
	}
	return nil
}

func DeleteTrack(path string, db *sql.DB) error {
	_, err := db.Exec(`DELETE FROM tracks WHERE path = ?`, path)
	return err
}

// Iterate through all albums in DB and delete albums with no tracks.
func RemoveEmptyAlbums(db *sql.DB) error {
	albumIDs, err := fetchAlbumIDs(db)
	if err != nil {
		return err
	}

	for _, id := range albumIDs {
		a := Album{ID: id}
		t, err := fetchTrackPaths(a.ID, db)
		if err != nil {
			return err
		}
		if len(t) < 1 {
			_, err := db.Exec(`DELETE FROM albums WHERE id = ?`, id)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func GetAlbumsOrderRandom(db *sql.DB) ([]Album, error) {
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

func sqliteOrder(asc bool) string {
	if asc {
		return "ASC"
	}
	return "DESC"
}

func GetAlbumsOrderAlbumArtist(asc bool, db *sql.DB) ([]Album, error) {
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

func FetchAlbumsByYear(asc bool, db *sql.DB) ([]Album, error) {
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

func FetchAlbumsByQuery(query string, db *sql.DB) ([]Album, error) {
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

func validateYearQuery(query []string) (int, int, error) {
	var a, b int

	if len(query) < 1 || len(query) > 2 {
		return a, b, errors.New("invalid year query")
	}

	a, err := strconv.Atoi(query[0])
	if err != nil {
		return a, b, fmt.Errorf("invalid year query: %s", query[0])
	}

	if len(query) == 2 {
		b, err = strconv.Atoi(query[1])
		if err != nil {
			return a, b, fmt.Errorf("invalid year query: %s", query[1])
		}
	}

	if b < a {
		return b, a, nil
	}

	return a, b, nil
}

func FindAlbumsByYear(query []string, db *sql.DB) ([]Album, error) {
	var rows *sql.Rows
	var err error

	a, b, err := validateYearQuery(query)
	if err != nil {
		return nil, err
	}

	if a > 0 {
		rows, err = db.Query(`SELECT * FROM albums WHERE
							year BETWEEN ? AND ?
							ORDER BY year ASC, album_artist ASC, name ASC;`, a, b)
	} else {
		rows, err = db.Query(`SELECT * FROM albums WHERE
							year = ?
							ORDER BY album_artist ASC, name ASC;`, b)
	}

	if err != nil {
		return nil, err
	}

	albums, err := parseRowsToAlbums(rows)
	if err != nil {
		return nil, err
	}

	return albums, nil
}

func PlayAlbum(albumID int64, execCmd string, showOut bool, showErr bool, db *sql.DB) error {
	paths, err := fetchTrackPaths(albumID, db)
	if err != nil {
		return err
	}
	c := strings.Split(execCmd, " ")
	args := append(c[1:], paths...)
	cmd := exec.Command(c[0], args...)
	if showOut {
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		err = startCmdWithOutput(cmd, stdout)
		if err != nil {
			return err
		}
	} else if showErr {
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return err
		}
		err = startCmdWithOutput(cmd, stderr)
		if err != nil {
			return err
		}
	} else {
		err := cmd.Start()
		if err != nil {
			return err
		}
	}
	return nil
}

func startCmdWithOutput(cmd *exec.Cmd, r io.ReadCloser) error {
	err := cmd.Start()
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		m := scanner.Text()
		fmt.Println(m)
	}
	err = cmd.Wait()
	if err != nil {
		return err
	}
	return nil
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

func isValidFileType(path string) bool {
	ext := filepath.Ext(path)
	switch strings.ToUpper(ext) {
	case
		".MP3", ".M4A", ".M4B", ".M4P", ".ALAC", ".FLAC", ".OGG", ".DSF":
		return true
	}
	return false
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

func insertTrack(t *Track, db *sql.DB) (int64, error) {
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

func fetchTrackPaths(albumID int64, db *sql.DB) ([]string, error) {
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

func FetchTrackPaths(db *sql.DB) ([]string, error) {
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
