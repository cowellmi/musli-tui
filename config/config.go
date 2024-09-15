package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	MusicDir    string
	ExecCmd     string
	CursorColor string
	PageLength  int
	Debug       bool
}

var DefaultConfig = Config{
	MusicDir:    filepath.Join("$HOME/Music"),
	ExecCmd:     "mpv",
	CursorColor: "202",
	PageLength:  10,
	Debug:       false,
}

func Load() (Config, error) {
	conf := DefaultConfig

	configDir, err := os.UserConfigDir()
	if err != nil {
		return conf, err
	}

	path := filepath.Join(configDir, "musli", "config.toml")

	err = os.MkdirAll(filepath.Dir(path), os.ModePerm)
	if err != nil {
		return conf, err
	}

	_, err = toml.DecodeFile(path, &conf)
	if err != nil {
		return conf, err
	}

	// Allows the use of $HOME and other env vars in the config.toml
	conf.MusicDir = os.ExpandEnv(conf.MusicDir)

	return conf, nil
}
