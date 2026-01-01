package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
)

const appDirName = "drive-client"

// Config holds basic runtime configuration.
type Config struct {
	AppName            string
	ConfigDir          string
	DataDir            string
	RuntimeDir         string
	SocketPath         string
	SyncRoot           string
	IgnorePatterns     []string
	EventLogSize       int
	SyncQueueSize      int
	LogLevel           string
	DatabasePath       string
	ConfigFile         string
	LogFilePath        string
	LogFileMaxMB       int
	LogFileMaxBackups  int
	LogFileMaxAgeDays  int
}

// NewConfig builds a default config from XDG paths and environment.
func NewConfig() (*Config, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		var err error
		configHome, err = os.UserConfigDir()
		if err != nil {
			return nil, err
		}
	}

	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dataHome = filepath.Join(home, ".local", "share")
	}

	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		runtimeDir = filepath.Join(home, ".cache")
	}

	if configHome == "" || dataHome == "" {
		return nil, errors.New("unable to resolve XDG directories")
	}

	configDir := filepath.Join(configHome, appDirName)
	dataDir := filepath.Join(dataHome, appDirName)
	socketPath := filepath.Join(runtimeDir, "googlysync", "daemon.sock")

	return &Config{
		AppName:           "googlysync",
		ConfigDir:         configDir,
		DataDir:           dataDir,
		RuntimeDir:        runtimeDir,
		SocketPath:        socketPath,
		SyncRoot:          filepath.Join(dataDir, "sync"),
		IgnorePatterns:    []string{"*.swp", "*.tmp", "*~", ".DS_Store"},
		EventLogSize:      20,
		SyncQueueSize:     1024,
		LogLevel:          "info",
		DatabasePath:      filepath.Join(dataDir, "googlysync.db"),
		LogFilePath:       filepath.Join(dataDir, "logs", "daemon.jsonl"),
		LogFileMaxMB:      10,
		LogFileMaxBackups: 5,
		LogFileMaxAgeDays: 7,
	}, nil
}

// Options defines runtime overrides for config resolution.
type Options struct {
	ConfigPath string
	LogLevel   string
	SocketPath string
}

type fileConfig struct {
	AppName           string   `json:"app_name"`
	ConfigDir         string   `json:"config_dir"`
	DataDir           string   `json:"data_dir"`
	RuntimeDir        string   `json:"runtime_dir"`
	SocketPath        string   `json:"socket_path"`
	SyncRoot          string   `json:"sync_root"`
	IgnorePatterns    []string `json:"ignore_patterns"`
	EventLogSize      int      `json:"event_log_size"`
	SyncQueueSize     int      `json:"sync_queue_size"`
	LogLevel          string   `json:"log_level"`
	DatabasePath      string   `json:"database_path"`
	LogFilePath       string   `json:"log_file_path"`
	LogFileMaxMB      int      `json:"log_file_max_mb"`
	LogFileMaxBackups int      `json:"log_file_max_backups"`
	LogFileMaxAgeDays int      `json:"log_file_max_age_days"`
}

// NewConfigWithOptions resolves config and applies overrides from options and environment.
func NewConfigWithOptions(opts Options) (*Config, error) {
	cfg, err := NewConfig()
	if err != nil {
		return nil, err
	}

	if opts.ConfigPath != "" {
		if err := applyConfigFile(cfg, opts.ConfigPath); err != nil {
			return nil, err
		}
		cfg.ConfigFile = opts.ConfigPath
	}

	applyEnv(cfg)

	if opts.LogLevel != "" {
		cfg.LogLevel = opts.LogLevel
	}
	if opts.SocketPath != "" {
		cfg.SocketPath = opts.SocketPath
	}

	return cfg, nil
}

func applyConfigFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return err
	}

	if fc.AppName != "" {
		cfg.AppName = fc.AppName
	}
	if fc.ConfigDir != "" {
		cfg.ConfigDir = fc.ConfigDir
	}
	if fc.DataDir != "" {
		cfg.DataDir = fc.DataDir
	}
	if fc.RuntimeDir != "" {
		cfg.RuntimeDir = fc.RuntimeDir
	}
	if fc.SocketPath != "" {
		cfg.SocketPath = fc.SocketPath
	}
	if fc.SyncRoot != "" {
		cfg.SyncRoot = fc.SyncRoot
	}
	if len(fc.IgnorePatterns) > 0 {
		cfg.IgnorePatterns = fc.IgnorePatterns
	}
	if fc.EventLogSize > 0 {
		cfg.EventLogSize = fc.EventLogSize
	}
	if fc.SyncQueueSize > 0 {
		cfg.SyncQueueSize = fc.SyncQueueSize
	}
	if fc.LogLevel != "" {
		cfg.LogLevel = fc.LogLevel
	}
	if fc.DatabasePath != "" {
		cfg.DatabasePath = fc.DatabasePath
	}
	if fc.LogFilePath != "" {
		cfg.LogFilePath = fc.LogFilePath
	}
	if fc.LogFileMaxMB > 0 {
		cfg.LogFileMaxMB = fc.LogFileMaxMB
	}
	if fc.LogFileMaxBackups > 0 {
		cfg.LogFileMaxBackups = fc.LogFileMaxBackups
	}
	if fc.LogFileMaxAgeDays > 0 {
		cfg.LogFileMaxAgeDays = fc.LogFileMaxAgeDays
	}

	return nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("GOOGLYSYNC_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("GOOGLYSYNC_LOG_FILE"); v != "" {
		cfg.LogFilePath = v
	}
	if v := os.Getenv("GOOGLYSYNC_LOG_MAX_MB"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			cfg.LogFileMaxMB = i
		}
	}
	if v := os.Getenv("GOOGLYSYNC_LOG_MAX_BACKUPS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			cfg.LogFileMaxBackups = i
		}
	}
	if v := os.Getenv("GOOGLYSYNC_LOG_MAX_AGE_DAYS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			cfg.LogFileMaxAgeDays = i
		}
	}
	if v := os.Getenv("GOOGLYSYNC_SOCKET_PATH"); v != "" {
		cfg.SocketPath = v
	}
	if v := os.Getenv("GOOGLYSYNC_SYNC_ROOT"); v != "" {
		cfg.SyncRoot = v
	}
	if v := os.Getenv("GOOGLYSYNC_IGNORE_PATTERNS"); v != "" {
		cfg.IgnorePatterns = splitList(v)
	}
	if v := os.Getenv("GOOGLYSYNC_EVENT_LOG_SIZE"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			cfg.EventLogSize = i
		}
	}
	if v := os.Getenv("GOOGLYSYNC_SYNC_QUEUE_SIZE"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			cfg.SyncQueueSize = i
		}
	}
}

func splitList(val string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(val); i++ {
		if i == len(val) || val[i] == ',' {
			if i > start {
				out = append(out, val[start:i])
			}
			start = i + 1
		}
	}
	return out
}
