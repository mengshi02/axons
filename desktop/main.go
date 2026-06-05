// Package main is the entry point for the axons desktop application.
//
// Architecture (Wails v3):
//   - The daemon runs its own HTTP server on a random TCP port (127.0.0.1:0).
//   - The daemon's HTTP server serves both the API routes (/api/*, /v1/*) and
//     the frontend static files (SPA with fallback to index.html).
//   - The Wails webview loads the daemon's URL directly — no AssetHandler needed.
//   - Frontend calls the daemon's HTTP API via fetch (same-origin, no CORS issues).
//   - No Wails bindings are used; the daemon's Go HTTP routes are the only backend.
//   - The Wails RawMessageHandler is used for desktop-specific IPC (e.g., open-external).
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/mengshi02/axons/internal/config"
	"github.com/mengshi02/axons/internal/daemon"
	"github.com/mengshi02/axons/internal/logger"
	"go.uber.org/zap"
)

const (
	appName        = "Axons"
	appDescription = "Secure Native Code Analysis & Generation Agent"
	websiteURL     = "https://www.axons.chat"
	issuesURL      = "https://github.com/mengshi02/axons/issues"
	releasesURL    = "https://github.com/mengshi02/axons/releases"
)

// daemonApp wraps the daemon lifecycle for the desktop application.
type daemonApp struct {
	daemon   *daemon.Daemon
	config   *config.Config
	httpAddr string
	listener net.Listener
}

// newDaemonApp creates a daemonApp with a random TCP port.
func newDaemonApp() (*daemonApp, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}
	return &daemonApp{
		httpAddr: listener.Addr().String(),
		listener: listener,
	}, nil
}

// start initializes and starts the daemon in the background.
func (d *daemonApp) start(ctx context.Context) {
	cfg := config.DefaultConfig()
	d.config = cfg

	logCfg := logger.Config{Debug: false, Output: ""}
	if err := logger.Initialize(logCfg); err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
	}

	dmn, err := daemon.New(cfg)
	if err != nil {
		logger.Error("Failed to create daemon", zap.Error(err))
		fmt.Printf("Failed to create daemon: %v\n", err)
		return
	}
	d.daemon = dmn
	d.daemon.SetTCPListener(d.listener)
	d.daemon.SetDesktopMode(true)

	go func() {
		if err := d.daemon.Run(); err != nil {
			logger.Error("Daemon exited with error", zap.Error(err))
		}
	}()

	// Wait for daemon HTTP server to be ready.
	for i := 0; i < 100; i++ {
		resp, err := http.Get(fmt.Sprintf("http://%s/api/health", d.httpAddr))
		if err == nil {
			resp.Body.Close()
			logger.Info("Desktop daemon ready", zap.String("addr", d.httpAddr))
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	logger.Error("Daemon HTTP server failed to start", zap.String("addr", d.httpAddr))
}

// stop shuts down the daemon.
func (d *daemonApp) stop() {
	logger.Info("Shutting down desktop app")
	if d.daemon != nil {
		d.daemon.Stop()
	}
	logger.Sync()
}

func main() {
	// Create daemon with random port
	app, err := newDaemonApp()
	if err != nil {
		fmt.Printf("Error creating app: %v\n", err)
		return
	}

	// Start daemon before creating Wails app
	app.start(context.Background())

	// Create Wails v3 application — webview loads daemon's URL directly.
	// No Services, no AssetHandler. The daemon's HTTP server handles everything.
	wailsApp := application.New(application.Options{
		Name:        appName,
		Description: appDescription,
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		RawMessageHandler: func(window application.Window, message string, originInfo *application.OriginInfo) {
			const prefix = "open-external:"
			if !strings.HasPrefix(message, prefix) {
				return
			}
			url := strings.TrimPrefix(message, prefix)
			if url == "" {
				return
			}
			globalApp.Browser.OpenURL(url)
		},
	})

	// Save references for menu callbacks
	globalApp = wailsApp
	daemonAddr = app.httpAddr

	// Set custom application menu
	wailsApp.Menu.SetApplicationMenu(newApplicationMenu())

	// Register shutdown hook
	wailsApp.OnShutdown(app.stop)

	// Create main window pointing to daemon's HTTP server
	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:           "Axons",
		Width:           1280,
		Height:          800,
		MinWidth:        1024,
		MinHeight:       600,
		URL:             fmt.Sprintf("http://%s", app.httpAddr),
		DevToolsEnabled: true,
		BackgroundType:  application.BackgroundTypeSolid,
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBar{
				AppearsTransparent:   true,
				Hide:                 false,
				HideTitle:            false,
				FullSizeContent:      true,
				UseToolbar:           false,
				HideToolbarSeparator: false,
			},
			Appearance: application.NSAppearanceNameAqua,
		},
		Windows: application.WindowsWindow{
			DisableIcon:                       false,
			DisableFramelessWindowDecorations: false,
			Theme:                             application.SystemDefault,
		},
	})

	if err := wailsApp.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

// globalApp holds a reference to the Wails application for menu callbacks.
var globalApp *application.App

// daemonAddr holds the daemon HTTP address for menu URL navigation.
var daemonAddr string

// newApplicationMenu creates the custom application menu bar for macOS.
func newApplicationMenu() *application.Menu {
	menu := application.NewMenu()

	// Axons app menu
	appMenu := menu.AddSubmenu(appName)
	appMenu.AddRole(application.About)
	appMenu.AddSeparator()
	appMenu.Add("Preferences...").
		SetAccelerator("CmdOrCtrl+,").
		OnClick(func(ctx *application.Context) {
			navigateTo("/settings")
		})
	appMenu.AddSeparator()
	appMenu.AddRole(application.ServicesMenu)
	appMenu.AddSeparator()
	appMenu.AddRole(application.Hide)
	appMenu.AddRole(application.HideOthers)
	appMenu.AddRole(application.UnHide)
	appMenu.AddSeparator()
	appMenu.AddRole(application.Quit)

	// File menu
	fileMenu := menu.AddSubmenu("File")
	fileMenu.Add("New Project").
		SetAccelerator("CmdOrCtrl+n").
		OnClick(func(ctx *application.Context) {
			navigateTo("/new")
		})
	fileMenu.Add("Open Project...").
		SetAccelerator("CmdOrCtrl+o").
		OnClick(func(ctx *application.Context) {
			navigateTo("/open")
		})
	fileMenu.AddSeparator()
	fileMenu.AddRole(application.CloseWindow)

	// Edit menu
	editMenu := menu.AddSubmenu("Edit")
	editMenu.AddRole(application.Undo)
	editMenu.AddRole(application.Redo)
	editMenu.AddSeparator()
	editMenu.AddRole(application.Cut)
	editMenu.AddRole(application.Copy)
	editMenu.AddRole(application.Paste)
	editMenu.AddRole(application.PasteAndMatchStyle)
	editMenu.AddRole(application.Delete)
	editMenu.AddRole(application.SelectAll)
	editMenu.AddSeparator()
	editMenu.AddRole(application.Find)
	editMenu.AddRole(application.FindNext)
	editMenu.AddRole(application.FindPrevious)

	// View menu
	viewMenu := menu.AddSubmenu("View")
	viewMenu.AddRole(application.Reload)
	viewMenu.AddRole(application.ForceReload)
	viewMenu.AddRole(application.OpenDevTools)
	viewMenu.AddSeparator()
	viewMenu.AddRole(application.ResetZoom)
	viewMenu.AddRole(application.ZoomIn)
	viewMenu.AddRole(application.ZoomOut)
	viewMenu.AddSeparator()
	viewMenu.AddRole(application.ToggleFullscreen)

	// Window menu (use default role)
	menu.AddRole(application.WindowMenu)

	// Help menu
	helpMenu := menu.AddSubmenu("Help")
	helpMenu.Add("Official Website").
		OnClick(func(ctx *application.Context) {
			openBrowser(websiteURL)
		})
	helpMenu.AddSeparator()
	helpMenu.Add("Report an Issue").
		OnClick(func(ctx *application.Context) {
			openBrowser(issuesURL)
		})
	helpMenu.Add("Release Notes").
		OnClick(func(ctx *application.Context) {
			openBrowser(releasesURL)
		})

	return menu
}

// navigateTo navigates the current window to a daemon-relative path.
func navigateTo(path string) {
	if globalApp == nil {
		return
	}
	currentWindow := globalApp.Window.Current()
	if currentWindow != nil {
		currentWindow.SetURL(fmt.Sprintf("http://%s%s", daemonAddr, path))
	}
}

// openBrowser opens the given URL in the default system browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default: // linux
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}