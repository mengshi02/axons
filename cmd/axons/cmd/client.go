// Package cmd provides CLI commands.
package cmd

import (
	"fmt"
	"os"

	"github.com/mengshi02/axons/internal/client"
	"github.com/mengshi02/axons/internal/config"
)

// getClient creates a client connected to the daemon.
// If daemon is not running, it returns an error.
func getClient() (*client.Client, error) {
	cfg, err := config.Load("")
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	c := client.New(cfg.SocketPath())

	// Check if daemon is running
	if err := c.Health(); err != nil {
		return nil, fmt.Errorf("daemon is not running. Start it with 'axons daemon start'")
	}

	return c, nil
}

// mustGetClient creates a client or exits with an error.
func mustGetClient() *client.Client {
	c, err := getClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	return c
}

// ensureDaemonRunning checks if daemon is running and exits if not.
func ensureDaemonRunning() {
	cfg, _ := config.Load("")
	c := client.New(cfg.SocketPath())
	if err := c.Health(); err != nil {
		fmt.Fprintln(os.Stderr, "Error: daemon is not running. Start it with 'axons daemon start'")
		os.Exit(1)
	}
}