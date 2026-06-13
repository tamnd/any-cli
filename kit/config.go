package kit

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config is the resolved runtime configuration kit hands a domain when it builds
// the client. The common fields live here so a domain does not re-derive XDG
// paths, rate, retries, and the like; a domain adds only its own extra settings
// (carried in Extra or read from its own env). Precedence is applied by the CLI
// surface: flags over env over the config file over these defaults.
type Config struct {
	Binary    string        // the binary name, e.g. "ccrawl"
	EnvPrefix string        // env var prefix, e.g. "CCRAWL"
	DataDir   string        // root for cache, store, sessions
	CacheDir  string        // on-disk content cache
	ConfigDir string        // config file directory
	Profile   string        // selected profile name
	Rate      time.Duration // minimum delay between requests
	Retries   int           // retry attempts on 429/5xx
	Timeout   time.Duration // per-request timeout
	Workers   int           // bulk concurrency
	NoCache   bool          // bypass caches
	Color     string        // auto|always|never
	UserAgent string        // descriptive default UA
	DB        string        // --db DSN for the record store (empty = no store)
	Verbose   int           // verbosity level
	Quiet     bool          // suppress progress
	DryRun    bool          // print actions, do not perform them
	Extra     map[string]string
}

// DefaultConfig builds the baseline config for a binary, deriving the XDG paths
// and env prefix from the name. A domain overlays its own defaults on top.
func DefaultConfig(binary, envPrefix string) Config {
	if envPrefix == "" {
		envPrefix = strings.ToUpper(binary)
	}
	data := dataDir(binary, envPrefix)
	return Config{
		Binary:    binary,
		EnvPrefix: envPrefix,
		DataDir:   data,
		CacheDir:  filepath.Join(data, "cache"),
		ConfigDir: configDir(binary, envPrefix),
		Rate:      time.Second,
		Retries:   3,
		Timeout:   30 * time.Second,
		Workers:   4,
		Color:     "auto",
		Extra:     map[string]string{},
	}
}

// Env reads a prefixed environment variable (e.g. CCRAWL_DATA_DIR for key
// "DATA_DIR"), returning ok=false when unset.
func (c Config) Env(key string) (string, bool) {
	return os.LookupEnv(c.EnvPrefix + "_" + key)
}

func dataDir(binary, prefix string) string {
	if v := os.Getenv(prefix + "_DATA_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, binary)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), binary)
	}
	return filepath.Join(home, ".local", "share", binary)
}

func configDir(binary, prefix string) string {
	if v := os.Getenv(prefix + "_CONFIG_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, binary)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), binary, "config")
	}
	return filepath.Join(home, ".config", binary)
}
