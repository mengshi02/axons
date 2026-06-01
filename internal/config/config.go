// Package config provides configuration management for axons daemon.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config holds all configuration for the axons daemon.
type Config struct {
	// Daemon configuration
	Daemon DaemonConfig `json:"daemon" yaml:"daemon"`

	// Database configuration
	Database DatabaseConfig `json:"database" yaml:"database"`

	// API configuration
	API APIConfig `json:"api" yaml:"api"`

	// Build configuration
	Build BuildConfig `json:"build" yaml:"build"`

	// Embed configuration
	Embed EmbedConfig `json:"embed" yaml:"embed"`

	// MCP configuration
	MCP MCPConfig `json:"mcp" yaml:"mcp"`

	// Agent configuration
	Agent AgentConfig `json:"agent" yaml:"agent"`

	// Terminal configuration
	Terminal TerminalConfig `json:"terminal" yaml:"terminal"`
}

// DaemonConfig holds daemon-specific configuration.
type DaemonConfig struct {
	// Listen address (supports unix:// and tcp://)
	Listen string `json:"listen" yaml:"listen"`

	// PID file path
	PIDFile string `json:"pid_file" yaml:"pid_file"`

	// Log file path
	LogFile string `json:"log_file" yaml:"log_file"`

	// Log level (debug, info, warn, error)
	LogLevel string `json:"log_level" yaml:"log_level"`

	// ClonesDir is the directory where cloned repositories are stored
	ClonesDir string `json:"clones_dir" yaml:"clones_dir"`
}

// DatabaseConfig holds database configuration.
type DatabaseConfig struct {
	// Path to the SQLite database file
	Path string `json:"path" yaml:"path"`

	// Connection pool size
	PoolSize int `json:"pool_size" yaml:"pool_size"`
}

// APIConfig holds API server configuration.
type APIConfig struct {
	// Enable TCP listener (optional, for WebUI)
	TCP string `json:"tcp" yaml:"tcp"`

	// Read timeout in seconds
	ReadTimeout int `json:"read_timeout" yaml:"read_timeout"`

	// Write timeout in seconds
	WriteTimeout int `json:"write_timeout" yaml:"write_timeout"`
}

// BuildConfig holds build configuration.
type BuildConfig struct {
	// Number of concurrent workers
	Concurrency int `json:"concurrency" yaml:"concurrency"`

	// Enable file watching
	Watch bool `json:"watch" yaml:"watch"`
}

// EmbedConfig holds embedding configuration.
type EmbedConfig struct {
	// Embedding model to use
	Model string `json:"model" yaml:"model"`

	// Batch size for embedding
	BatchSize int `json:"batch_size" yaml:"batch_size"`
}

// MCPConfig holds MCP server configuration.
type MCPConfig struct {
	// Enable MCP server
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Transport mode (stdio, websocket)
	Transport string `json:"transport" yaml:"transport"`
}

// AgentConfig holds AI agent configuration.
type AgentConfig struct {
	// Enable agent service
	Enabled bool `json:"enabled" yaml:"enabled"`

	// LLM provider (openai, anthropic, ollama)
	Provider string `json:"provider" yaml:"provider"`

	// API key for the LLM provider
	APIKey string `json:"api_key" yaml:"api_key"`

	// Model name
	Model string `json:"model" yaml:"model"`

	// Base URL (optional, for custom endpoints)
	BaseURL string `json:"base_url" yaml:"base_url"`

	// Max rounds for tool calls
	MaxRounds int `json:"max_rounds" yaml:"max_rounds"`

	// System prompt (optional)
	SystemPrompt string `json:"system_prompt" yaml:"system_prompt"`
}

// TerminalConfig holds terminal configuration.
type TerminalConfig struct {
	// Enable terminal feature
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Maximum number of sessions per user
	MaxSessions int `json:"max_sessions" yaml:"max_sessions"`

	// Session timeout in minutes
	SessionTimeout int `json:"session_timeout" yaml:"session_timeout"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	axonsDir := filepath.Join(homeDir, ".axons")

	return &Config{
		Daemon: DaemonConfig{
			Listen:   "unix://" + filepath.Join(axonsDir, "daemon.sock"),
			PIDFile:  filepath.Join(axonsDir, "daemon.pid"),
			LogFile:  filepath.Join(axonsDir, "daemon.log"),
			LogLevel: "info",
			ClonesDir: filepath.Join(axonsDir, "repos"),
		},
		Database: DatabaseConfig{
			Path:     filepath.Join(axonsDir, "axons.db"),
			PoolSize: 10,
		},
		API: APIConfig{
			ReadTimeout:  30,
			WriteTimeout: 0, // Disabled for SSE streams - users can cancel if response is too slow
		},
		Build: BuildConfig{
			Concurrency: 4,
			Watch:       false,
		},
		Embed: EmbedConfig{
			Model:     "text-embedding-3-small",
			BatchSize: 100,
		},
		MCP: MCPConfig{
			Enabled:   true,
			Transport: "stdio",
		},
		Agent: AgentConfig{
			Enabled:   false,
			Provider:  "openai",
			Model:     "gpt-4o",
			MaxRounds: 10,
		},
		Terminal: TerminalConfig{
			Enabled:        true,
			MaxSessions:    20, // Increased from 5 to 20
			SessionTimeout: 30, // 30 minutes
		},
	}
}

// Load loads configuration from a file.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		return cfg, nil
	}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}

	// TODO: Support YAML/JSON config file loading
	return cfg, nil
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Daemon.Listen == "" {
		return fmt.Errorf("daemon listen address is required")
	}
	if c.Database.Path == "" {
		return fmt.Errorf("database path is required")
	}
	return nil
}

// SocketPath returns the Unix socket path from the listen address.
func (c *Config) SocketPath() string {
	listen := c.Daemon.Listen
	if len(listen) > 7 && listen[:7] == "unix://" {
		return listen[7:]
	}
	return listen
}

// EnsureDirs ensures all necessary directories exist.
func (c *Config) EnsureDirs() error {
	dirs := []string{
		filepath.Dir(c.Daemon.PIDFile),
		filepath.Dir(c.Daemon.LogFile),
		filepath.Dir(c.Database.Path),
	}

	for _, dir := range dirs {
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dir, err)
			}
		}
	}
	return nil
}