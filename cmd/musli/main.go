package main

import (
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"

	"github.com/BurntSushi/toml"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gosuri/uilive"
	"github.com/micahco/musli"
	"golang.org/x/term"
)

const (
	APP_NAME     = "musli"
	CURSOR_COLOR = "5"
	CELL_WIDTH   = 15
)

type config struct {
	MusicDir    string
	ExecCmd     string
	CursorColor string
	PageLength  int
	ShowStdout  bool
	ShowStderr  bool
}

func main() {
	exitCode := 0
	defer func() {
		os.Exit(exitCode)
	}()

	err := root(os.Args[1:])
	if err != nil {
		fmt.Println(APP_NAME + ": " + err.Error())
		exitCode = 1
	}
}

func root(args []string) error {
	conf, err := loadConfig()
	if err != nil {
		return err
	}

	db, err := loadDB()
	if err != nil {
		return err
	}
	defer musli.CloseDB(db)

	if len(args) == 0 {
		m, err := initialModel(conf, db)
		if err != nil {
			return err
		}
		if m == nil {
			// no albums in library
			return nil
		}
		p := tea.NewProgram(m)
		_, err = p.Run()
		return err
	}

	switch arg := args[0]; arg {
	case "-h", "--help":
		printUsage()
	case "-r", "--random":
		albums, err := musli.GetAlbumsOrderRandom(db)
		if err != nil {
			return err
		}
		randAlbum := albums[rand.IntN(100)]
		musli.PlayAlbum(randAlbum.ID, conf.ExecCmd, conf.ShowStdout, conf.ShowStderr, db)
	case "-s", "--scan":
		err = execScan(conf, db)
	case "-t", "--tidy":
		err = execTidy(db)
	default:
		return fmt.Errorf("invalid option: '%s'", arg)
	}
	if err != nil {
		return err
	}
	return nil
}

func printUsage() {
	fmt.Printf("Usage of %s:", APP_NAME)
	fmt.Println(`
-r, --random: play random album from library
-s, --scan: scan music directory for new files
-t, --tidy: scrub library for entries that no longer exist`)
}

func loadConfig() (*config, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, APP_NAME, "config.toml")

	err = os.MkdirAll(filepath.Dir(path), os.ModePerm)
	if err != nil {
		return nil, err
	}

	conf, err := readConfig(path)
	if err != nil {
		return nil, err
	}

	return conf, nil
}

func readConfig(path string) (*config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	conf := config{ // Default values
		MusicDir:    filepath.Join(home, "Music"),
		ExecCmd:     "mpv",
		CursorColor: CURSOR_COLOR,
		PageLength:  10,
		ShowStdout:  false,
		ShowStderr:  false,
	}

	_, err = toml.DecodeFile(path, &conf)
	if err != nil {
		return nil, err
	}

	// Allows the use of $HOME and other env vars in the config.toml
	conf.MusicDir = os.ExpandEnv(conf.MusicDir)

	return &conf, nil
}

func loadDB() (*sql.DB, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, APP_NAME, "library.db")

	err = os.MkdirAll(filepath.Dir(path), os.ModePerm)
	if err != nil {
		return nil, err
	}

	db, err := musli.OpenDB(path)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func execScan(conf *config, db *sql.DB) error {
	fmt.Println("Scanning:", conf.MusicDir)

	w := uilive.New()
	w.Start()

	paths, err := musli.GetMusicDirPaths(conf.MusicDir)
	if err != nil {
		return err
	}
	total := len(paths)

	for i, path := range paths {
		fmt.Fprintf(w, "%d/%d\n", i, total)
		err = musli.AddPathToLibrary(path, db)
		if err != nil {
			return err
		}
	}

	fmt.Fprintln(w, "Scanned", total, "files")

	w.Stop()
	return nil
}

func execTidy(db *sql.DB) error {
	fmt.Println("Scrubbing library")

	w := uilive.New()
	w.Start()

	paths, err := musli.FetchTrackPaths(db)
	if err != nil {
		return err
	}
	total := len(paths)

	for i, path := range paths {
		fmt.Fprintf(w, "%d/%d\n", i, total)
		_, err := os.Stat(path)
		// Check if path actually exists
		if errors.Is(err, os.ErrNotExist) {
			musli.DeleteTrack(path, db)
		}
		if err != nil {
			return err
		}
	}

	err = musli.RemoveEmptyAlbums(db)
	if err != nil {
		return err
	}

	fmt.Fprintln(w, "Scrubbed", total, "files")
	w.Stop()
	return nil
}

var sortMethods = [3]string{"random", "artist", "year"}

const (
	sortMethodRandom = iota
	sortMethodArtist
	sortMethodYear
)

func fetchAlbums(db *sql.DB, sortMethod int, asc bool) ([]musli.Album, error) {
	var albums []musli.Album
	var err error
	switch sortMethod {
	case sortMethodRandom:
		albums, err = musli.GetAlbumsOrderRandom(db)
	case sortMethodArtist:
		albums, err = musli.GetAlbumsOrderAlbumArtist(asc, db)
	case sortMethodYear:
		albums, err = musli.FetchAlbumsByYear(asc, db)
	}
	return albums, err
}

type model struct {
	albums     []musli.Album
	conf       *config
	db         *sql.DB
	filter     bool
	sortMethod int
	sortAsc    bool // true = ascending; false = descending
	query      string
	start      int // page start index
	cursor     int // page cursor index
}

var styleCursor = lipgloss.NewStyle().
	Foreground(lipgloss.Color(CURSOR_COLOR))
var styleBold = lipgloss.NewStyle().
	Bold(true)
var styleAlbums = lipgloss.NewStyle().
	MarginLeft(1).
	TabWidth(5)
var styleHeader = lipgloss.NewStyle().
	Border(lipgloss.NormalBorder())
var styleHeaderCell = lipgloss.NewStyle().
	Width(CELL_WIDTH)

func initialModel(conf *config, db *sql.DB) (*model, error) {
	albums, err := musli.GetAlbumsOrderRandom(db)
	if err != nil {
		return nil, err
	}

	if len(albums) == 0 {
		err := execScan(conf, db)
		if err != nil {
			return nil, err
		}
		albums, err = musli.GetAlbumsOrderRandom(db)
		if err != nil {
			return nil, err
		}
		if len(albums) == 0 {
			return nil, nil
		}
	}

	if term.IsTerminal(0) {
		tw, _, err := term.GetSize(0)
		if err != nil {
			return nil, err
		}
		styleHeader.Width(tw - 2) // subtract 2 for border width
	}

	styleCursor.Foreground(lipgloss.Color(conf.CursorColor))
	m := &model{
		albums:     albums,
		conf:       conf,
		db:         db,
		filter:     false,
		query:      "",
		sortMethod: 0,
		sortAsc:    true,
		start:      0,
		cursor:     0,
	}

	return m, nil
}

func (m *model) Init() tea.Cmd {
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m.quit(nil)
		}
		var cmd tea.Cmd
		var err error
		if m.filter {
			cmd, err = m.controllerFilter(key)
		} else {
			cmd, err = m.controllerMain(key)
		}
		if err != nil {
			return m.quit(err)
		}
		if cmd != nil {
			return m, cmd
		}
	}
	if m.filter && len(m.query) > 0 {
		m.start = 0
		albums, err := musli.FetchAlbumsByQuery(m.query, m.db)
		if err != nil {
			return m.quit(err)
		}
		m.albums = albums
	}
	return m, nil
}

func (m *model) View() string {
	s := m.viewHeader()
	s += m.viewAlbums()
	return s
}

func (m *model) quit(err error) (tea.Model, tea.Cmd) {
	if err != nil {
		fmt.Println("musli: ", err.Error())
	}
	return m, tea.Quit
}

func (m *model) controllerMain(key string) (tea.Cmd, error) {
	switch key {
	case "q":
		return tea.Quit, nil
	case "pgup":
		m.moveStart()
	case "pgdown":
		m.moveEnd()
	case "left", "h":
		m.moveLeft()
	case "up", "k":
		m.moveUp()
	case "down", "j":
		m.moveDown()
	case "right", "l":
		m.moveRight()
	case "o":
		if len(m.query) == 0 && m.sortMethod != sortMethodRandom {
			m.sortAsc = !m.sortAsc
			albums, err := fetchAlbums(m.db, m.sortMethod, m.sortAsc)
			if err != nil {
				return nil, err
			}
			m.albums = albums
		}
	case "s":
		if len(m.query) == 0 {
			err := m.toggleSortMethod()
			if err != nil {
				return nil, err
			}
		}
	case "/":
		m.cursor = -1
		m.filter = true
	case "enter", " ":
		album := m.albums[m.start+m.cursor]
		err := musli.PlayAlbum(album.ID, m.conf.ExecCmd, m.conf.ShowStdout, m.conf.ShowStderr, m.db)
		if err != nil {
			return nil, err
		}
	case "esc":
		if len(m.query) > 0 {
			err := m.clearFilter()
			if err != nil {
				return nil, err
			}
		}
	}
	return nil, nil
}

func (m *model) controllerFilter(key string) (tea.Cmd, error) {
	switch key {
	case "backspace":
		if len(m.query) > 0 {
			// remove last char from query
			m.query = m.query[:len(m.query)-1]
		}
	case "enter":
		m.filter = false
		m.start = 0
		m.cursor = 0
		if len(m.query) == 0 {
			albums, err := fetchAlbums(m.db, m.sortMethod, m.sortAsc)
			if err != nil {
				return nil, err
			}
			m.albums = albums
		}
	case "esc":
		m.filter = false
		err := m.clearFilter()
		if err != nil {
			return nil, err
		}
	default:
		if len(key) == 1 {
			m.query += key
		}
	}
	return nil, nil
}

func (m *model) clearFilter() error {
	m.cursor = 0
	m.query = ""
	albums, err := fetchAlbums(m.db, m.sortMethod, m.sortAsc)
	if err != nil {
		return err
	}
	m.albums = albums
	return nil
}

func (m *model) viewHeader() string {
	cur := m.start/m.conf.PageLength + 1
	total := len(m.albums)/m.conf.PageLength + 1
	pg := styleBold.Render("pg: ")
	pg += strconv.Itoa(cur) + " / " + strconv.Itoa(total)
	s := styleHeaderCell.Render(pg)
	if m.filter || len(m.query) > 0 {
		s += styleBold.Render("query: ") + m.query
		if m.filter {
			s += styleBold.Render("_")
		}
	} else if !m.filter && len(m.query) == 0 {
		sort := styleBold.Render("sort: ") + sortMethods[m.sortMethod]
		s += styleHeaderCell.Render(sort)
		if m.sortMethod != sortMethodRandom {
			s += styleBold.Render("order: ")
			if m.sortAsc {
				s += "asc"
			} else {
				s += "desc"
			}
		}
	}
	return styleHeader.Render(s) + "\n"
}

func (m *model) viewAlbums() string {
	if len(m.albums) == 0 {
		return styleAlbums.Render("no results")
	}
	var s string
	y := 0 // current year value
	for i := m.start; i < m.start+m.conf.PageLength && i < len(m.albums); i++ {
		a := m.albums[i]
		if m.sortMethod == sortMethodYear && !m.filter && len(m.query) == 0 {
			if a.Year != y {
				y = a.Year
				s += styleBold.Render(strconv.Itoa(y)) + " "
			} else {
				s += "\t"
			}
		}
		as := a.AlbumArtist + " - " + a.Name
		if m.start+m.cursor == i && !m.filter {
			as = styleCursor.Render(as)
		}
		s += as + "\n"
	}
	return styleAlbums.Render(s)
}

func (m *model) toggleSortMethod() error {
	m.sortMethod++
	if m.sortMethod >= len(sortMethods) {
		m.sortMethod = 0
	}
	albums, err := fetchAlbums(m.db, m.sortMethod, m.sortAsc)
	if err != nil {
		return err
	}
	m.albums = albums
	return nil
}

func (m *model) moveLeft() {
	m.start -= m.conf.PageLength
	if m.start < 0 {
		m.start = 0
	}
}

func (m *model) moveUp() {
	if m.cursor > 0 {
		m.cursor--
	}
}

func (m *model) moveDown() {
	if m.cursor < m.conf.PageLength-1 && m.start+m.cursor < len(m.albums)-1 {
		m.cursor++
	}
}

func (m *model) moveRight() {
	if m.start+m.conf.PageLength < len(m.albums)-1 {
		m.start += m.conf.PageLength
	}
	if m.start+m.cursor > len(m.albums)-1 {
		m.cursor = len(m.albums) - m.start - 1
	}
}

func (m *model) moveStart() {
	m.start = 0
}

func (m *model) moveEnd() {
	i := m.start
	for i+m.conf.PageLength < len(m.albums) {
		i += m.conf.PageLength
	}
	m.start = i
}
