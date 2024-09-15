package main

import (
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosuri/uilive"
	"github.com/micahco/musli/api"
	"github.com/micahco/musli/config"
	"github.com/micahco/musli/ui"
)

func main() {
	exitCode := 0
	defer os.Exit(exitCode)

	err := root()
	if err != nil {
		fmt.Fprintf(os.Stderr, "musli: %v", err)
		exitCode = 1
	}
}

func root() error {
	conf, err := config.Load()
	if err != nil {
		return err
	}

	lib, err := api.NewLibrary()
	if err != nil {
		return err
	}
	defer lib.Close()

	m, err := ui.NewModel(conf, lib)
	if err != nil {
		return err
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()

	return err
}

// TODO: dry
func execScan(conf config.Config, lib api.Library) error {
	fmt.Println("Scanning:", conf.MusicDir)

	w := uilive.New()
	w.Start()

	paths, err := lib.FindAudioFilePaths(conf.MusicDir)
	if err != nil {
		return err
	}
	total := len(paths)

	for i, path := range paths {
		// limit writes
		w.Flush()
		fmt.Fprintf(w, "%d/%d\n", i, total)
		err = lib.AddPathToLibrary(path)
		if err != nil {
			return err
		}
	}

	w.Flush()
	fmt.Fprintln(w, "Scanned", total, "files")
	w.Stop()

	return nil
}

func execTidy(lib api.Library) error {
	fmt.Println("Scrubbing library")

	w := uilive.New()
	w.Start()

	paths, err := lib.AllTrackPaths()
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
			lib.DeleteTrack(path)
		}
		if err != nil {
			return err
		}
	}

	err = lib.RemoveEmptyAlbums()
	if err != nil {
		return err
	}

	w.Flush()
	fmt.Fprintln(w, "Scrubbed", total, "files")
	w.Stop()

	return nil
}
