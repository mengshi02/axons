# Axons Desktop Application

This directory contains the desktop application version of Axons, built with [Wails v3](https://wails.io/).

## Prerequisites

- Go 1.25+
- Node.js 22+
- Wails v3 CLI: `go install github.com/wailsapp/wails/v3/cmd/wails3@latest`

### Platform-Specific Requirements

**macOS:**
- Xcode Command Line Tools: `xcode-select --install`

**Windows:**
- WebView2 Runtime (included in Windows 10/11)
- For installer creation: [NSIS](https://nsis.sourceforge.io/)

**Linux:**
- `libwebkit2gtk-4.1-dev` (or `libwebkit2gtk-4.0-dev` depending on distribution)
- `build-essential` (gcc, make, etc.)
- `libgtk-3-dev`
- `libglib2.0-dev`

Debian/Ubuntu:
```bash
sudo apt install libwebkit2gtk-4.1-dev build-essential libgtk-3-dev libglib2.0-dev
```

Fedora:
```bash
sudo dnf install webkit2gtk4.1-devel gcc gcc-c++ gtk3-devel glib2-devel
```

Arch Linux:
```bash
sudo pacman -S webkit2gtk-4.1 base-devel gtk3 glib2
```

## Quick Start

```bash
# Check dependencies
make doctor

# Run in development mode
make dev

# Build for current platform
make build
```

## Build Targets

### Development

```bash
make dev              # Run with hot reload
make doctor           # Check dependencies
```

### macOS

```bash
make build-mac              # Build for current architecture
make build-mac-arm64        # Build for Apple Silicon
make build-mac-amd64        # Build for Intel Mac
make build-mac-dmg          # Build DMG installer
```

### Windows

```bash
make build-windows           # Build executable
make build-windows-installer # Build NSIS installer
```

### Linux

```bash
make build-linux             # Build executable
make build-linux-appimage    # Build AppImage package
```

### Cleanup

```bash
make clean        # Clean build artifacts
```

## Project Structure

```
desktop/
├── main.go              # Application entry point
├── build/
│   ├── appicon.png      # Application icon source (1024x1024)
│   ├── config.yml       # Wails v3 dev mode configuration
│   ├── Taskfile.yml     # Common build tasks
│   ├── create-dmg.sh    # macOS DMG creation script
│   ├── create-windows-installer.sh  # Windows NSIS installer script
│   ├── darwin/
│   │   ├── Taskfile.yml # macOS build tasks
│   │   ├── Info.plist   # macOS app bundle metadata
│   │   └── icons.icns   # macOS icon (generated)
│   ├── linux/
│   │   ├── Taskfile.yml   # Linux build tasks
│   │   ├── axons.desktop  # Linux desktop entry
│   │   └── AppRun         # AppImage launcher script
│   └── windows/
│       ├── Taskfile.yml         # Windows build tasks
│       ├── wails.exe.manifest   # Windows app manifest (DPI awareness)
│       ├── info.json            # Windows exe metadata
│       ├── installer.nsi        # NSIS installer script
│       └── icon.ico             # Windows icon (generated)
├── Taskfile.yml         # Top-level task definitions
├── wails.json           # Wails project metadata
├── Makefile             # Build commands
└── README.md            # This file
```

## Architecture

The desktop application uses a **daemon-first** architecture — no Wails bindings:

1. **Daemon HTTP server** — Starts on a random TCP port (`127.0.0.1:0`), serves both the API routes (`/api/*`, `/v1/*`) and the frontend static files (SPA with index.html fallback)
2. **Wails webview** — Loads the daemon's URL directly (`http://127.0.0.1:PORT`)
3. **Frontend** — Calls the daemon's HTTP API via `fetch` (same-origin, no CORS issues)

This approach allows:
- Full code reuse from the CLI/Web version
- No Wails-specific bindings or generated code needed
- Easy debugging (can also access via browser at the same URL)

## Development Tips

1. **Hot Reload**: Run `make dev` for frontend hot reload
2. **Browser Debug**: Open DevTools in the app window (Cmd/Ctrl+Shift+I)
3. **Logging**: Check console output for daemon logs

## Taskfile (Wails v3 Build System)

Wails v3 uses [Taskfile](https://taskfile.dev/) instead of the v2 CLI's built-in build commands. Key tasks:

```bash
wails3 task build       # Build the application
wails3 task package     # Package (macOS: .app, Windows: NSIS installer, Linux: AppImage)
wails3 task dev         # Development mode with hot reload
wails3 task dmg         # Create macOS DMG (macOS only)
```

## Troubleshooting

### "wails3: command not found"

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
```

### macOS: "cannot verify developer"

This is expected for unsigned builds. To bypass:
1. Right-click the app and select "Open"
2. Or run: `xattr -cr bin/axons.app`

### Windows: SmartScreen warning

This is expected for unsigned builds. Click "More info" then "Run anyway".

### Linux: Missing webkit2gtk

If you see errors about missing webkit2gtk, install the development package:

```bash
# Debian/Ubuntu
sudo apt install libwebkit2gtk-4.1-dev

# Fedora
sudo dnf install webkit2gtk4.1-devel
```

## Code Signing (Production)

For production releases, you'll need:

**macOS:**
- Apple Developer account ($99/year)
- `codesign` and `notarytool` for signing and notarization

**Windows:**
- Code signing certificate ($100-500/year)
- `signtool` for signing

**Linux:**
- AppImage: No code signing required by default
- For deb/rpm distribution: GPG signing is recommended

## License

See root LICENSE file.