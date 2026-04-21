package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"log-monitoring/internal/app"
	"log-monitoring/internal/config"
)

var debugLog bool

func main() {
	var (
		remote     = flag.Bool("remote", false, "Enable remote server monitoring mode")
		serversCfg = flag.String("servers", "", "Path to servers.yaml config file")
		timeout    = flag.Duration("timeout", 10*time.Second, "SSH connection timeout")
		debug      = flag.Bool("debug", false, "Enable debug logging")
	)
	flag.Parse()

	if *debug {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}

	// Local mode (default)
	if !*remote {
		m := app.New()
		p := tea.NewProgram(m, tea.WithAltScreen())

		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Remote mode
	cfgPath := *serversCfg
	if cfgPath == "" {
		cfgPath = os.Getenv("LOGMON_SERVERS")
	}
	if cfgPath == "" {
		// Try to find servers.yaml relative to executable
		exePath, err := os.Executable()
		if err == nil {
			// Try same directory as binary
			cfgPath = exePath[:len(exePath)-len("log-monitoring")] + "servers.yaml"
		}
	}
	if cfgPath == "" {
		cfgPath = "servers.yaml"
	}

	slog.Info("loading server config", "path", cfgPath)

	// Create default config if missing
	if err := config.EnsureConfig(cfgPath); err != nil {
		slog.Warn("could not create default config", "err", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Edit %s and add your server definitions.\n", cfgPath)
		fmt.Fprintf(os.Stderr, "Example:\n%s\n", config.Example())
		os.Exit(1)
	}

	if len(cfg.Servers) == 0 {
		fmt.Fprintf(os.Stderr, "No servers defined in %s\n", cfgPath)
		fmt.Fprintf(os.Stderr, "Add your server definitions. Example:\n%s\n", config.Example())
		os.Exit(1)
	}

	m, err := app.NewRemote(cfg.Servers, *timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing remote mode: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
