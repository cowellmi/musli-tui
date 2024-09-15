package api

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

func isAudioFile(path string) bool {
	ext := filepath.Ext(path)
	switch strings.ToUpper(ext) {
	case
		".MP3", ".M4A", ".M4B", ".M4P", ".ALAC", ".FLAC", ".OGG", ".DSF":
		return true
	}
	return false
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

func sqliteOrder(asc bool) string {
	if asc {
		return "ASC"
	}
	return "DESC"
}
