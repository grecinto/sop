# SOP AI Desktop - Electron App

An always-on-top floating AI chat window for macOS, Windows, and Linux.

## Features

- **Always-on-Top Chat Window**: Float the AI chat above all other windows (VS Code, terminals, browsers, etc.)
- **System Tray Integration**: Minimize to tray and quickly toggle chat window
- **Full App Window**: Access complete SOP AI interface when needed
- **Zero Backend Changes**: Uses existing httpserver backend

## Architecture

```
electron-app/
├── main.js           # Electron main process (window management)
├── preload.js        # Security bridge (context isolation)
├── package.json      # Dependencies and build config
└── README.md

Flow:
1. Electron app starts → Spawns httpserver Go binary
2. Creates two windows:
   - Main window: Full UI at http://localhost:8080
   - Chat window: Chat-only at http://localhost:8080/chat (alwaysOnTop: true)
3. System tray icon for quick access
```

## Prerequisites

- **Node.js** 18+ (for Electron)
- **Go** 1.21+ (already have this)
- **Built httpserver binary** at `../httpserver/httpserver`

## Setup

### 1. Install Dependencies

```bash
cd /Volumes/T7/sop-cloud/lib/sop/tools/electron-app
npm install
```

### 2. Build httpserver Binary

```bash
cd ../httpserver
go build
chmod +x httpserver
```

### 3. Run in Development Mode

```bash
cd ../electron-app
npm start
```

or with dev flag:

```bash
npm run dev
```

This will:
- Start the httpserver backend on port 8080
- Open the main window with full UI
- Open the floating chat window (always on top)
- Create a system tray icon

## Usage

### Floating Chat Window

The chat window is **always on top** of all other windows. Perfect for:
- Asking questions while coding in VS Code
- Getting help while reading documentation
- Quick RAG queries without switching windows

**Keyboard Shortcuts** (coming soon):
- `Cmd/Ctrl + Shift + Space`: Toggle chat window
- `Cmd/Ctrl + Shift + M`: Show main window

### System Tray

Right-click the tray icon:
- **Show Main Window**: Open full SOP AI interface
- **Toggle Chat Window**: Show/hide floating chat
- **Quit**: Exit application

Double-click tray icon to toggle chat window.

## Building for Production

### Package for macOS

```bash
npm run package
```

This creates a `.dmg` and `.zip` in the `dist/` folder containing:
- Electron app bundle
- Embedded httpserver binary
- All templates and assets

The packaged app is fully self-contained.

### Customization

Edit `package.json` build section:

```json
{
  "build": {
    "mac": {
      "icon": "assets/icon.icns",  // Add your icon
      "category": "public.app-category.productivity"
    },
    "win": {
      "icon": "assets/icon.ico"
    }
  }
}
```

## Development

### Hot Reload

The app uses the built httpserver binary, so:
1. Make changes to httpserver Go code
2. Rebuild: `cd ../httpserver && go build`
3. Restart Electron app: `npm start`

For UI changes (templates/), just refresh the Electron window:
- Main window: `Cmd/Ctrl + R`
- Chat window: `Cmd/Ctrl + R`

### Debugging

**Main Process (Electron):**
```bash
# Logs appear in terminal where you ran npm start
console.log('Debug main process')
```

**Renderer Process (Web UI):**
```bash
# Use Chrome DevTools
# In main.js, add: mainWindow.webContents.openDevTools()
```

**httpserver Logs:**
```bash
# Logs appear in Electron terminal with [httpserver] prefix
```

## Window Behavior

### Chat Window
- **Size**: 420x650px
- **Position**: Bottom-right corner (20px padding)
- **Always On Top**: Yes
- **Resizable**: Yes (vertical)
- **Minimizable**: Yes
- **Frameless**: No (has title bar for dragging)

### Main Window
- **Size**: 1400x900px
- **Position**: Center screen
- **Always On Top**: No
- **Full Features**: All SOP AI capabilities

## API Integration

Both windows connect to the same httpserver backend at `http://localhost:8080`.

### Chat Window Routes:
- `GET /chat` - Chat-only UI
- `POST /api/ai/chat` - Send messages

### Main Window Routes:
- `GET /` - Full SOP AI interface
- All existing API routes work normally

## Security

- **Context Isolation**: Enabled
- **Node Integration**: Disabled in renderer
- **Secure IPC**: Uses `contextBridge` for communication
- **Local Only**: Backend only listens on localhost

## Troubleshooting

### "httpserver not found"
```bash
# Build the binary
cd ../httpserver
go build
chmod +x httpserver
```

### Port 8080 already in use
Edit `main.js`:
```javascript
const PORT = 8081;  // Change port
```

### Chat window not always on top
Check your OS settings:
- macOS: System Preferences → Accessibility (might block)
- Windows: No special permissions needed
- Linux: Depends on window manager

### httpserver crashes on startup
Check logs in terminal:
```bash
npm start 2>&1 | tee electron-debug.log
```

## Production Deployment

### macOS

1. **Code Signing** (for distribution):
```bash
export APPLE_ID=your@email.com
export APPLE_ID_PASSWORD=your-app-specific-password
npm run package
```

2. **Notarization** (for Gatekeeper):
```bash
npx electron-notarize --bundle-id com.sopcloud.ai-desktop
```

### Windows

1. **Install Certificate** (optional, for trusted installer)
2. **Build**:
```bash
npm run package
```

Creates `SOP-AI-Desktop-Setup.exe` in `dist/`

## Future Enhancements

- [ ] Global keyboard shortcuts
- [ ] Custom window positioning
- [ ] Multiple chat windows
- [ ] Auto-updates
- [ ] Custom themes
- [ ] Offline mode
- [ ] Chat history persistence
- [ ] Screenshot/clipboard integration

## License

Same as SOP Cloud project

## Support

For issues, see main SOP Cloud repository.
