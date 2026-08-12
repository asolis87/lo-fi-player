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

	// historyCap es el maximo de entradas que retaine el historial
	// migrado desde v1/versionless (HIST-3 escenario cap). El
	// migrador opera sobre el slice legacy y trunca una vez
	// alcanzada la cota, sin tocar I/O.
	historyCap = 25
)

// ErrCorrupt is wrapped around parse failures so the corruption
// recovery path can detect a malformed file with errors.Is.
var ErrCorrupt = errors.New("config: file is corrupted")

type Config struct {
	LastQueue      []string
	LastTrackIndex int
	Volume         int
	// History es el MRU de IDs reproducidos (newest-first, cap 25);
	// lo consume PlaybackState (VOL-1 + HIST-1 + HIST-2) y solo se
	// emite en el esquema v2 (SCHEMA-1). En una Config construida
	// a partir de legacy v1/versionless, History queda vacio y se
	// pobla via migrateLegacyToV2.
	History []string
}

// Default devuelve un Config con volumen inicial 50 (VOL-1) e
// historial vacio; el caller debe poblar History via
// migrateLegacyToV2 cuando cargue desde legacy v1/versionless.
func Default() Config { return Config{Volume: DefaultVolume} }

// renameFile es el seam de renaming atomico inyectable (PERSIST-2):
// permite que las pruebas fuercen una falla controlada antes del
// rename para demostrar que el staging se limpia y el archivo
// previo queda intacto. Por defecto usa os.Rename.
var renameFile = os.Rename

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

// Load lee el config. Ausente => Default(). Strict version router (SCHEMA-1):
// schema_version=2 -> parseV2; ausente -> legacy parse; v3+/unknown-key/malformed
// => ErrCorrupt (LoadOrDefault quarantine a .bak).
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
	// Sniff de schema_version: primera linea con "<key> = <int>".
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 || strings.TrimSpace(line[:eq]) != schemaVersionKey {
			continue
		}
		var vN int
		if json.Unmarshal([]byte(strings.TrimSpace(line[eq+1:])), &vN) != nil {
			break
		}
		if vN != 2 {
			return nil, fmt.Errorf("%w: schema_version %d unsupported (only v2)", ErrCorrupt, vN)
		}
		h, vol, verr := parseV2(data)
		if verr != nil {
			return nil, fmt.Errorf("%w: %s", ErrCorrupt, verr.Error())
		}
		return &Config{Volume: vol, History: h}, nil
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
	// History presente => v2 estricto; ausente => v1/versionless (SCHEMA-1 + HIST-3).
	var werr error
	if cfg.History != nil {
		werr = writeV2(stage, cfg.History, cfg.Volume)
	} else {
		werr = writeTOML(stage, cfg)
	}
	if werr != nil {
		return werr
	}
	if err := stage.Sync(); err != nil {
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	if err := renameFile(stagePath, target); err != nil {
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

// schemaVersionKey marca el router estricto de parseV2; cualquier
// valor numerico distinto de 2 produce un error de quarantine
// (SCHEMA-1 escenario 2: futuras versiones no se cargan
// parcialmente).
const schemaVersionKey = "schema_version"

// parseV2 implementa el lector estricto de la v2 (SCHEMA-1): solo
// reconoce schema_version = 2, volume y history (array de strings).
// Cualquier otra clave de nivel superior, contenido malformado o
// valor distinto a 2 produce error; el caller decide ejecutar la
// cuarentena (quarantineInvalidConfig). El lector nunca es
// permisivo y nunca omite silenciosamente claves desconocidas.
func parseV2(data []byte) (history []string, volume int, err error) {
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			return nil, 0, fmt.Errorf("schema_version: line without '=' near %q", raw)
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		switch key {
		case schemaVersionKey:
			var n int
			if uerr := json.Unmarshal([]byte(value), &n); uerr != nil {
				return nil, 0, fmt.Errorf("schema_version: %w", uerr)
			}
			if n != 2 {
				return nil, 0, fmt.Errorf("schema_version %d unsupported (only v2)", n)
			}
		case "volume":
			if uerr := json.Unmarshal([]byte(value), &volume); uerr != nil {
				return nil, 0, fmt.Errorf("volume: %w", uerr)
			}
		case "history":
			if uerr := json.Unmarshal([]byte(value), &history); uerr != nil {
				return nil, 0, fmt.Errorf("history: %w", uerr)
			}
			if history == nil {
				history = []string{}
			}
		default:
			return nil, 0, fmt.Errorf("unknown key %q", key)
		}
	}
	return history, volume, nil
}

// writeV2 emite EXCLUSIVAMENTE los campos del esquema v2: la clave
// legacy last_queue nunca aparece (HIST-3 + SCHEMA-1). El orden es
// fijo para que el archivo sea byte-estable entre releases.
func writeV2(w io.Writer, history []string, volume int) error {
	if history == nil {
		history = []string{}
	}
	historyLit, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("config: marshal history: %w", err)
	}
	for _, ln := range []string{
		"# lofi-player config (schema v2)",
		fmt.Sprintf("%s = 2", schemaVersionKey),
		fmt.Sprintf("volume = %d", volume),
		fmt.Sprintf("history = %s", historyLit),
	} {
		if _, err := fmt.Fprintln(w, ln); err != nil {
			return err
		}
	}
	return nil
}

// migrateLegacyToV2 convierte el orden de insercion legacy
// (oldest -> newest) en MRU-front (newest -> newest first), elimina
// duplicados reteniendo la primera ocurrencia en orden legacy y
// limita a 25 (HIST-3). No toca I/O: opera sobre slices, lo que
// permite siembra directa en tests y hace la politica deterministica
// y facil de auditar. El algoritmo es dedupe-first-then-reverse:
// caminamos la lista legacy reteniendo la primera ocurrencia de cada
// id, y luego invertimos el slice resultante para producir MRU-front.
func migrateLegacyToV2(lastQueue []string) []string {
	if len(lastQueue) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(lastQueue))
	dedup := make([]string, 0, len(lastQueue))
	for _, id := range lastQueue {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		dedup = append(dedup, id)
		if len(dedup) == historyCap {
			break
		}
	}
	if len(dedup) == 0 {
		return []string{}
	}
	reversed := make([]string, len(dedup))
	for i, v := range dedup {
		reversed[len(dedup)-1-i] = v
	}
	return reversed
}

// quarantineInvalidConfig mueve un archivo inválido a <path>.bak.
// Es la extension helper que usa SCHEMA-1 cuando parseV2 detecta
// claves desconocidas, versiones futuras o contenido malformado:
// el caller detecta el error y delega el movimiento a esta funcion
// para mantener una sola politica de recovery en el paquete.
func quarantineInvalidConfig(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("config: stat %s: %w", path, err)
	}
	if err := os.Rename(path, path+backupSuffix); err != nil {
		return fmt.Errorf("config: quarantine %s: %w", path, err)
	}
	return nil
}
