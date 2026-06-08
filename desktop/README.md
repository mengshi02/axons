# Axons Desktop Application

This directory contains the desktop application version of Axons, built with [Electron](https://www.electronjs.org/).

## Prerequisites

- Go 1.25+
- Node.js 22+

### Platform-Specific Requirements

**macOS:**
- Xcode Command Line Tools: `xcode-select --install`

**Windows:**
- WebView2 Runtime (included in Windows 10/11)
- For installer creation: [NSIS](https://nsis.sourceforge.io/)

**Linux:**
- No additional requirements beyond standard desktop libraries

## Quick Start

```bash
# Install npm dependencies
cd desktop && npm install

# Run in development mode
make dev

# Build for current platform
make build
```

## Build Targets

### Development

```bash
make dev              # Run with hot reload
```

### macOS

```bash
make build-mac              # Build for current architecture
make build-mac-arm64        # Build for Apple Silicon
make build-mac-amd64        # Build for Intel Mac
make dist-mac               # Package macOS app (DMG + ZIP)
```

### Windows

```bash
make dist-win               # Build and package Windows app (NSIS installer)
```

### Linux

```bash
make dist-linux             # Build and package Linux app (AppImage + deb)
```

### Cleanup

```bash
make clean        # Clean build artifacts
```

## Project Structure

```
desktop/
├── main.go              # Application entry point (unused in Electron build)
├── build/
│   ├── appicon.png      # Application icon source (1024x1024)
│   ├── entitlements.mac.plist  # macOS entitlements for code signing
│   ├── generate-icons.js       # Icon generation script
│   ├── convert-icon.sh         # SVG to PNG icon conversion
│   ├── darwin/
│   │   └── icons.icns   # macOS icon (generated)
│   └── windows/
│       ├── icon.ico     # Windows icon (generated)
│       └── icon-*.png   # Windows icon sizes (generated)
├── src/
│   ├── main.ts          # Electron main process
│   ├── preload.ts       # Preload script (contextBridge)
│   └── electron.d.ts    # TypeScript declarations
├── Makefile             # Build commands
├── package.json         # Node.js dependencies & electron-builder config
└── README.md            # This file
```

## Architecture

The desktop application uses a **daemon-first** architecture:

1. **Daemon HTTP server** — Starts on a random TCP port (`127.0.0.1:0`), serves both the API routes (`/api/*`, `/v1/*`) and the frontend static files (SPA with index.html fallback)
2. **Electron BrowserWindow** — Loads the daemon's URL directly (`http://127.0.0.1:PORT`)
3. **Frontend** — Calls the daemon's HTTP API via `fetch` (same-origin, no CORS issues)

This approach allows:
- Full code reuse from the CLI/Web version
- Easy debugging (can also access via browser at the same URL)

## Development Tips

1. **Hot Reload**: Run `make dev` for development mode
2. **Browser Debug**: Open DevTools in the app window (Cmd/Ctrl+Shift+I)
3. **Logging**: Check console output for daemon logs

## License

See root LICENSE file.