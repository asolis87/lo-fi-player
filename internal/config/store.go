// Package config persists lo-fi-player user settings: XDG paths,
// atomic TOML writes, corruption quarantine.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	dirName      = "lofi-player"
	fileName     = "config.toml"
	backupSuffix = ".bak"
	tempPrefix   = ".config.toml."
	dirPerm      = 0o700
	filePerm     = 0o600
)

// ErrCorrupt is wrapped around parse failures so the corruption
// recovery path can detect a malformed file with errors.Is.
var ErrCorrupt = errors.New("config: file is corrupted")

type Config struct {
	LastQueue      []string
	LastTrackIndex int
	Volume         int
}

func Default() Config { return Config{Volume: 80} }

func Dir() (string, error) {
	base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("config: resolve home: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, dirName), nil
}

func Path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, fileName), nil
}

// Load reads the config. Missing file returns Default() (first run).
// Malformed file returns ErrCorrupt so LoadOrDefault can quarantine it.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			d := Default()
			return &d, nil
		}
		return nil, fmt.Errorf("config: read %s: %w", p, err)
	}
	cfg, perr := parse(data)
	if perr != nil {
		return nil, fmt.Errorf("%w: %s", ErrCorrupt, perr.Error())
	}
	return &cfg, nil
}

// Save writes cfg atomically: ensure dir, stage into a temp file in
// the same filesystem, fsync + close, then os.Rename over the
// target. On any error path before rename the staging file is
// removed so the directory never accumulates orphans.
func Save(cfg *Config) error {
	if cfg == nil {
		return errors.New("config: Save called with nil config")
	}
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("config: mkdir %s: %w", dir, err)
	}
	target, err := Path()
	if err != nil {
		return err
	}
	stage, err := os.CreateTemp(dir, tempPrefix)
	if err != nil {
		return fmt.Errorf("config: create staging file: %w", err)
	}
	stagePath := stage.Name()
	var renamed bool
	defer func() {
		if !renamed {
			os.Remove(stagePath)
		}
	}()
	if err := os.Chmod(stagePath, filePerm); err != nil {
		return err
	}
	if err := writeTOML(stage, cfg); err != nil {
		return err
	}
	if err := stage.Sync(); err != nil {
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	if err := os.Rename(stagePath, target); err != nil {
		return err
	}
	renamed = true
	return nil
}

// LoadOrDefault never errors and never panics. A corrupted file is
// moved aside to <name>.bak and the caller gets Default().
func LoadOrDefault() *Config {
	cfg, err := Load()
	if err == nil {
		return cfg
	}
	p, perr := Path()
	if perr == nil && errors.Is(err, ErrCorrupt) {
		_ = os.Rename(p, p+backupSuffix)
	}
	d := Default()
	return &d
}

// parse reads the documented subset of TOML (top-level keys,
// integers, JSON-shaped strings and string arrays). Serialisation
// goes through encoding/json so escape semantics stay consistent
// with the rest of the Go ecosystem.
func parse(data []byte) (Config, error) {
	cfg := Default()
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			return Config{}, fmt.Errorf("line without '=' near %q", raw)
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		switch key {
		case "last_track_index", "volume":
			var n int
			if err := json.Unmarshal([]byte(value), &n); err != nil {
				return Config{}, fmt.Errorf("%s: %w", key, err)
			}
			if key == "volume" {
				cfg.Volume = n
			} else {
				cfg.LastTrackIndex = n
			}
		case "last_queue":
			if err := json.Unmarshal([]byte(value), &cfg.LastQueue); err != nil {
				return Config{}, fmt.Errorf("last_queue: %w", err)
			}
			if cfg.LastQueue == nil {
				cfg.LastQueue = []string{}
			}
		default:
			return Config{}, fmt.Errorf("unknown key %q", key)
		}
	}
	return cfg, nil
}

// writeTOML serialises cfg using JSON for string and array values
// (TOML accepts both shapes inline). Output order is fixed so the
// file is byte-stable across releases.
func writeTOML(w io.Writer, cfg *Config) error {
	queue := cfg.LastQueue
	if queue == nil {
		queue = []string{}
	}
	queueLit, err := json.Marshal(queue)
	if err != nil {
		return fmt.Errorf("config: marshal queue: %w", err)
	}
	for _, ln := range []string{
		"# lofi-player config",
		fmt.Sprintf("last_queue = %s", queueLit),
		fmt.Sprintf("last_track_index = %d", cfg.LastTrackIndex),
		fmt.Sprintf("volume = %d", cfg.Volume),
	} {
		if _, err := fmt.Fprintln(w, ln); err != nil {
			return err
		}
	}
	return nil
}
