package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gosuri/uilive"
	"github.com/micahco/musli"
	"golang.org/x/term"
)

const (
	APP_NAME     = "musli"
	CURSOR_COLOR = "202"
	CELL_WIDTH   = 15
)

type config struct {
	MusicDir    string
	ExecCmd     string
	CursorColor string
	PageLength  int
	Debug       bool
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
		album, err := musli.GetOneRandomAlbum(db)
		if err != nil {
			return err
		}
		if album == nil {
			return fmt.Errorf("empty library")
		}
		playAlbum(db, album.ID, conf.ExecCmd, conf.Debug)
	case "-s", "--scan":
		err = execScan(conf, db)
	case "-t", "--tidy":
		err = execTidy(db)
	default:
		return fmt.Errorf("invalid option: '%s'", arg)
	}
	return err
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
		Debug:       false,
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

// TODO: dry
func execScan(conf *config, db *sql.DB) error {
	fmt.Println("Scanning:", conf.MusicDir)

	w := uilive.New()
	w.Start()

	paths, err := musli.FindAudioFilePaths(conf.MusicDir)
	if err != nil {
		return err
	}
	total := len(paths)

	for i, path := range paths {
		// limit writes
		w.Flush()
		fmt.Fprintf(w, "%d/%d\n", i, total)
		err = musli.AddPathToLibrary(db, path)
		if err != nil {
			return err
		}
	}

	w.Flush()
	fmt.Fprintln(w, "Scanned", total, "files")
	w.Stop()

	return nil
}

func execTidy(db *sql.DB) error {
	fmt.Println("Scrubbing library")

	w := uilive.New()
	w.Start()

	paths, err := musli.AllTrackPaths(db)
	if err != nil {
		return err
	}
	total := len(paths)

	for i, path := range paths {
		w.Flush()
		fmt.Fprintf(w, "%d/%d\n", i, total)
		// check if file exists
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			musli.DeleteTrack(db, path)
		}
		if err != nil {
			return err
		}
	}

	err = musli.RemoveEmptyAlbums(db)
	if err != nil {
		return err
	}

	w.Flush()
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

func sortedAlbums(db *sql.DB, sortMethod int, asc bool) ([]musli.Album, error) {
	var albums []musli.Album
	var err error
	switch sortMethod {
	case sortMethodRandom:
		albums, err = musli.GetRandomAlbums(db)
	case sortMethodArtist:
		albums, err = musli.GetAlbumsByArtist(db, asc)
	case sortMethodYear:
		albums, err = musli.GetAlbumsByYear(db, asc)
	}
	return albums, err
}

func playAlbum(db *sql.DB, albumID int64, execCmd string, debug bool) error {
	paths, err := musli.AlbumTrackPaths(db, albumID)
	if err != nil {
		return err
	}

	c := strings.Split(execCmd, " ")
	args := append(c[1:], paths...)
	cmd := exec.Command(c[0], args...)

	if debug {
		ct := time.Now()
		ft := ct.Format("2006-01-02_15-04-05")
		name := fmt.Sprintf("%s_%s.txt", APP_NAME, ft)

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
	log        bool
}

var (
	styleCursor = lipgloss.NewStyle().
			Foreground(lipgloss.Color(CURSOR_COLOR))
	styleBold = lipgloss.NewStyle().
			Bold(true)
	styleHeader = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder())
	styleAlbums = lipgloss.NewStyle().
			MarginLeft(1).
			TabWidth(5)
	styleLog        = lipgloss.NewStyle()
	styleHeaderCell = lipgloss.NewStyle().
			Width(CELL_WIDTH)
)

func initialModel(conf *config, db *sql.DB) (*model, error) {
	albums, err := musli.GetRandomAlbums(db)
	if err != nil {
		return nil, err
	}

	if len(albums) == 0 {
		err := execScan(conf, db)
		if err != nil {
			return nil, err
		}
		albums, err = musli.GetRandomAlbums(db)
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
		// accomodate for border width
		styleHeader.Width(tw - 2)
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
	return tea.ClearScreen
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
		albums, err := musli.SearchAlbums(m.db, m.query)
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
	s += m.viewLog()
	return s
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

func (m *model) viewLog() string {
	if !m.log {
		return ""
	}
	s := "\nLOG:\n"
	return styleLog.Render(s)
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
	case "c": // clear screen
		return tea.ClearScreen, nil
	case "left", "h":
		m.moveLeft()
	case "up", "k":
		m.moveUp()
	case "down", "j":
		m.moveDown()
	case "right", "l":
		m.moveRight()
	case "o": // order
		if len(m.query) == 0 && m.sortMethod != sortMethodRandom {
			m.sortAsc = !m.sortAsc
			albums, err := sortedAlbums(m.db, m.sortMethod, m.sortAsc)
			if err != nil {
				return nil, err
			}
			m.albums = albums
		}
	case "s": // sort
		if len(m.query) == 0 {
			err := m.toggleSortMethod()
			if err != nil {
				return nil, err
			}
		}
	case "/": // filter
		m.cursor = -1 // reset cursor
		m.filter = true
	case "enter", " ":
		album := m.albums[m.start+m.cursor]
		err := playAlbum(m.db, album.ID, m.conf.ExecCmd, m.conf.Debug)
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
			albums, err := sortedAlbums(m.db, m.sortMethod, m.sortAsc)
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
	albums, err := sortedAlbums(m.db, m.sortMethod, m.sortAsc)
	if err != nil {
		return err
	}
	m.albums = albums
	return nil
}

func (m *model) toggleSortMethod() error {
	m.sortMethod++
	if m.sortMethod >= len(sortMethods) {
		m.sortMethod = 0
	}
	albums, err := sortedAlbums(m.db, m.sortMethod, m.sortAsc)
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
