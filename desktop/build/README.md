# Build Assets

Place your application icon here as `appicon.png`.

## Requirements

- Format: PNG
- Size: 1024x1024 pixels (recommended)

## Icon Generation

Icons for each platform are auto-generated from `appicon.png` during the build:

```bash
wails3 generate icons -input appicon.png -macfilename darwin/icons.icns -windowsfilename windows/icon.ico
```

- macOS: `darwin/icons.icns`
- Windows: `windows/icon.ico`

## Tips

- Use a simple, recognizable design
- Ensure the icon looks good at small sizes (16x16, 32x32)
- Test the icon on both light and dark backgrounds