# musli — music library

An opinionated music library TUI focused on albums.

## 

## Options

Run `musli --help` for a list of options and how to use them.

## Configuration

musli uses a [TOML](https://toml.io/en/v1.0.0) configuration file. Each key has a default value.

The config file is located at: `~/.config/musli/config.toml` on Unix systems. This file will not be created automatically.

See [config.toml](https://github.com/micahco/musli/blob/main/config.toml) for an example.

### Parameters

#### MusicDir

Default: `"$HOME/Music"`

Recursively find music files in said directory.

#### ExecCmd

Default: `"mpv"`

Command to be executed for album playback. The individual paths for each track of the selected album will be passed as arguments to the command.

The command executed will look something like this:

`mpv /path/to/ablum/track1.mp3 /path/to/ablum/track2.mp3 ...`

#### CursorColor

Default: `"202"`

Color code that will highlight the cursor text in the TUI.

Refer to [Lip Gloss](https://github.com/charmbracelet/lipgloss?tab=readme-ov-file#colors) for valid color codes.

#### Debug:

Default: `false`

Outputs the `ExecCmd` stdout/stderr to a log file `musli_2006-01-02_15-04-05.txt` in the working directory.

## Compatible media players

Any media player that follows the following pattern should work.

`cmd [options] files...`

Tested:

* mpv
* vlc
* mplayer
* parole

Flatpak versions should work as well:


