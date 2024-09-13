# musli — music library

An extremely minimal TUI for albums.

* Fast
* Intuitive keybindings
* Uses your favorite media player
* Works well with network shares

## Options

Run `musli --help` for a list of options and how to use them.

## Configuration

`musli` requires a valid [TOML](https://toml.io/en/v1.0.0) configuration file to run.

* Unix: `$XDG_CONFIG_HOME/musli/config.toml`
    * Else: `$HOME/.config/musli/config.toml`
* Darwin: `$HOME/Library/Application Support/musli/config.toml`
* Windows: `%AppData%/musli/config.toml`

Please refer to [os.UserConfigDir](https://pkg.go.dev/os#UserConfigDir) for further details.

Each config parameter has a default value. See [config.toml](https://github.com/micahco/musli/blob/main/config.toml) for an example.

### Parameters

#### MusicDir

Default: `"$HOME/Music"`

Directory containing your music files. This will be recursively  scanned. You may access local environment variables (such as `$HOME`). Must use Unix-style forward slashes, even on Windows systems. For example, to access a public Windows share: `//Share/public/music`.

#### ExecCmd

Default: `"mpv"`

Command to be executed for album playback. The individual paths for each track of the selected album will be passed as arguments to the command. See [Compatible Media Players](#compatible-media-players) for more details.

#### CursorColor

Default: `"202"`

Color code that will highlight the active cursor text. Refer to [Lip Gloss](https://github.com/charmbracelet/lipgloss?tab=readme-ov-file#colors) for valid color codes.

#### Debug:

Default: `false`

If true, outputs the `ExecCmd` stdout/stderr to a log file `musli_2006-01-02_15-04-05.txt` in the working directory. This will create a new log file each time the command is executed (or; whenever an album is played).

## Compatible Media Players

Any media player that uses the following CLI pattern should work.

`cmd [options] files...`

Valid `ExecCmd` values:

* `mpv`
* `vlc`
* `mplayer`
* `parole`
* `flatpak run io.mpv.Mpv`
