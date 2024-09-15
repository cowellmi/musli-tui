package ui

import (
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/micahco/musli/api"
	"github.com/micahco/musli/config"
)

type Model struct {
	lib  api.Library
	conf config.Config

	albums     []api.Album
	filter     bool
	sortMethod int
	sortAsc    bool // true = ascending; false = descending
	query      string
	start      int
	cursor     int
}

var (
	styleCursor = lipgloss.NewStyle().
			Foreground(lipgloss.Color("CURSOR_COLOR"))
	styleBold = lipgloss.NewStyle().
			Bold(true)
	styleHeader = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder())
	styleAlbums = lipgloss.NewStyle().
			MarginLeft(1).
			TabWidth(5)
	styleHeaderCell = lipgloss.NewStyle().
			Width(5)
)

func NewModel(conf config.Config, lib api.Library) (Model, error) {
	styleCursor.Foreground(lipgloss.Color(conf.CursorColor))
	m := Model{
		lib:        lib,
		conf:       conf,
		filter:     false,
		query:      "",
		sortMethod: 0,
		sortAsc:    true,
		start:      0,
		cursor:     0,
	}

	var err error
	m.albums, err = lib.GetRandomAlbums()
	if err != nil {
		return m, err
	}

	return m, nil
}

func (m Model) Init() tea.Cmd {
	return tea.ClearScreen
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		albums, err := m.lib.SearchAlbums(m.query)
		if err != nil {
			return m.quit(err)
		}
		m.albums = albums
	}

	return m, nil
}

func (m Model) View() string {
	s := m.viewHeader()
	s += m.viewAlbums()
	return s
}

func (m Model) viewHeader() string {
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

func (m Model) viewAlbums() string {
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

func (m Model) quit(err error) (tea.Model, tea.Cmd) {
	if err != nil {
		fmt.Println("musli: ", err.Error())
	}
	return m, tea.Quit
}

func (m Model) controllerMain(key string) (tea.Cmd, error) {
	switch key {
	case "q":
		return tea.Quit, nil
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
			albums, err := m.sortedAlbums(m.sortMethod, m.sortAsc)
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
		err := m.lib.PlayAlbum(album.ID, m.conf.ExecCmd, m.conf.Debug)
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

func (m Model) controllerFilter(key string) (tea.Cmd, error) {
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
			albums, err := m.sortedAlbums(m.sortMethod, m.sortAsc)
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

func (m Model) clearFilter() error {
	m.cursor = 0
	m.query = ""
	albums, err := m.sortedAlbums(m.sortMethod, m.sortAsc)
	if err != nil {
		return err
	}
	m.albums = albums
	return nil
}

func (m Model) toggleSortMethod() error {
	m.sortMethod++
	if m.sortMethod >= len(sortMethods) {
		m.sortMethod = 0
	}
	albums, err := m.sortedAlbums(m.sortMethod, m.sortAsc)
	if err != nil {
		return err
	}
	m.albums = albums
	return nil
}

func (m Model) moveLeft() {
	m.start -= m.conf.PageLength
	if m.start < 0 {
		m.start = 0
	}
}

func (m Model) moveUp() {
	if m.cursor > 0 {
		m.cursor--
	}
}

func (m Model) moveDown() {
	if m.cursor < m.conf.PageLength-1 && m.start+m.cursor < len(m.albums)-1 {
		m.cursor++
	}
}

func (m Model) moveRight() {
	if m.start+m.conf.PageLength < len(m.albums)-1 {
		m.start += m.conf.PageLength
	}
	if m.start+m.cursor > len(m.albums)-1 {
		m.cursor = len(m.albums) - m.start - 1
	}
}

var sortMethods = [3]string{"random", "artist", "year"}

const (
	sortMethodRandom = iota
	sortMethodArtist
	sortMethodYear
)

func (m Model) sortedAlbums(sortMethod int, asc bool) ([]api.Album, error) {
	var albums []api.Album
	var err error
	switch sortMethod {
	case sortMethodRandom:
		albums, err = m.lib.GetRandomAlbums()
	case sortMethodArtist:
		albums, err = m.lib.GetAlbumsByArtist(asc)
	case sortMethodYear:
		albums, err = m.lib.GetAlbumsByYear(asc)
	}
	return albums, err
}
