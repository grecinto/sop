# SOP AI Desktop Application

## Overview

The SOP AI Desktop application provides an **always-on-top floating chat window** that stays above all desktop applications (VS Code, terminals, browsers, etc.). This is built using Electron as a wrapper around the existing httpserver backend.

## Architecture

```
┌─────────────────────────────────────────┐
│   Electron Desktop App                  │
│                                          │
│  ┌────────────────┐  ┌───────────────┐ │
│  │  Main Window   │  │  Chat Window  │ │
│  │  (Full UI)     │  │  (Floating)   │ │
│  │  1400x900      │  │  420x650      │ │
│  └────────────────┘  │  alwaysOnTop  │ │
│                      └───────────────┘ │
│                                          │
│         ↓ HTTP Requests ↓                │
│                                          │
│  ┌────────────────────────────────────┐ │
│  │  httpserver (Go Binary)            │ │
│  │  - Runs as child process           │ │
│  │  - Port 8080                       │ │
│  │  - Full sopbackend APIs            │ │
│  └────────────────────────────────────┘ │
└─────────────────────────────────────────┘
```

### Key Features

1. **Always-on-Top Chat**: Floating chat window stays above all applications
2. **System Tray Integration**: Quick access via menu bar icon
3. **Full UI Access**: Main window provides complete SOP AI interface
4. **Cross-Platform**: macOS, Windows, Linux support
5. **Self-Contained**: Packaged app has zero external dependencies

## For Developers

### Prerequisites (Development Only)

- Node.js 18+
- npm (comes with Node.js)
- Go 1.21+ (for building httpserver)

### Development Setup

**Quick Start:**
```bash
cd /Volumes/T7/sop-cloud/lib/sop/tools/electron-app
./start.sh
```

The `start.sh` script handles everything:
- Checks prerequisites
- Installs dependencies if needed
- Builds httpserver if needed  
- Launches the app

**Manual Setup:**
```bash
# Install Node.js
brew install node  # macOS
# or download from https://nodejs.org/

# Install dependencies
cd tools/electron-app
npm install

# Build httpserver
cd ../httpserver
go build

# Launch app
cd ../electron-app
npm start
```

### Project Structure

```
tools/electron-app/
├── main.js              # Electron main process
│                        # - Window management
│                        # - Process spawning (httpserver)
│                        # - System tray
│                        # - IPC handlers
├── preload.js           # Security bridge (context isolation)
├── package.json         # Dependencies & build config
├── start.sh            # Automated setup script
└── README.md           # Detailed documentation

tools/httpserver/
├── main.go             # HTTP server with /chat route
├── templates/
│   ├── chat.html       # Minimal chat-only UI
│   └── ...            # Other UI templates
└── httpserver         # Compiled binary (packaged with app)
```

### Development Workflow

1. **Make changes to backend** (Go code):
   ```bash
   cd tools/httpserver
   go build
   # Restart Electron app
   ```

2. **Make changes to UI** (templates):
   - Just press `Cmd/Ctrl + R` in Electron window to reload
   - No rebuild needed

3. **Make changes to Electron** (main.js, preload.js):
   - Restart: `npm start`

### Debugging

**Electron Main Process:**
- Logs appear in terminal where `npm start` ran
- Use `console.log()` in main.js

**Renderer Process (Web UI):**
- Open DevTools: Add `mainWindow.webContents.openDevTools()` in main.js
- Or press `Cmd/Ctrl + Shift + I` in window

**httpserver Logs:**
- Appear in terminal with `[httpserver]` prefix
- Standard httpserver logging

## For End Users

### Installation (Production)

**No dependencies required!** Users just:

1. Download the app:
   - macOS: `SOP-AI-Desktop.dmg`
   - Windows: `SOP-AI-Desktop-Setup.exe`  
   - Linux: `SOP-AI-Desktop.AppImage`

2. Install/Run:
   - macOS: Drag to Applications folder, double-click
   - Windows: Run installer, launch from Start menu
   - Linux: Make executable, double-click

The packaged app is **completely self-contained**:
- ✅ Electron runtime included
- ✅ httpserver binary included
- ✅ All UI templates included
- ✅ No Node.js, npm, Go required
- ✅ ~50-100MB total size

### Usage

**Floating Chat Window:**
- Always stays on top of other applications
- Position: Bottom-right corner by default
- Resizable vertically
- Can be minimized

**Main Window:**
- Full SOP AI interface
- Access all features (databases, stores, spaces, AI, etc.)
- Standard window behavior

**System Tray:**
- Right-click: Show menu (Show Main, Toggle Chat, Quit)
- Double-click: Toggle chat window visibility

**Keyboard Shortcuts** (future):
- `Cmd/Ctrl + Shift + Space`: Toggle chat
- `Cmd/Ctrl + Shift + M`: Show main window

## Building for Production

### Create Packaged App

```bash
cd tools/electron-app
npm run package
```

This creates distributable apps in `dist/`:
- macOS: `.dmg` and `.zip`
- Windows: `.exe` installer
- Linux: `.AppImage`

### Build Configuration

Defined in `package.json`:

```json
{
  "build": {
    "appId": "com.sopcloud.ai-desktop",
    "productName": "SOP AI Desktop",
    "mac": {
      "category": "public.app-category.productivity",
      "target": ["dmg", "zip"]
    },
    "extraResources": [
      {
        "from": "../httpserver/httpserver",
        "to": "bin/httpserver"
      },
      {
        "from": "../httpserver/templates",
        "to": "templates"
      }
    ]
  }
}
```

### Code Signing (macOS)

For distribution outside the App Store:

```bash
# Set up signing credentials
export APPLE_ID=your@email.com
export APPLE_ID_PASSWORD=your-app-specific-password

# Package with signing
npm run package
```

### Notarization (macOS)

Required for Gatekeeper approval:

```bash
npx electron-notarize \
  --bundle-id com.sopcloud.ai-desktop \
  --apple-id your@email.com \
  --apple-id-password your-app-specific-password
```

## Technical Details

### Window Management

**Chat Window:**
- Size: 420x650px
- Position: Bottom-right with 20px padding
- `alwaysOnTop: true` - OS-level always on top
- `frame: true` - Has title bar for dragging
- `resizable: true` - Vertical resize only
- `skipTaskbar: false` - Shows in dock/taskbar

**Main Window:**
- Size: 1400x900px
- Position: Center screen
- Standard window behavior
- Full feature set

### Process Management

Electron spawns httpserver as child process:

```javascript
httpserverProcess = spawn('./httpserver', [
  '--port', '8080',
  '--production'
]);

// Stdout/stderr piped to Electron terminal
// Killed on app exit
```

### Security

- **Context Isolation**: Enabled (renderer can't access Node.js directly)
- **Node Integration**: Disabled in renderer
- **IPC Bridge**: Uses `contextBridge` for secure communication
- **Local Only**: httpserver binds to localhost only

### API Integration

Both windows connect to same httpserver backend:

**Chat Window** (`/chat`):
- Minimal UI, chat only
- POST `/api/ai/chat` - Send messages
- No authentication required (local app)

**Main Window** (`/`):
- Full SOP AI interface
- All existing API routes
- Session management
- Database operations

## Troubleshooting

### Development Issues

**"httpserver not found"**
```bash
cd tools/httpserver
go build
chmod +x httpserver
```

**"Port 8080 already in use"**
- Stop other httpserver instances
- Or change port in `main.js`: `const PORT = 8081;`

**"Node.js not found"**
```bash
brew install node
# or download from nodejs.org
```

**Chat window not always on top (macOS)**
- Check System Preferences → Accessibility
- May need to grant permissions

### Production Issues

**macOS: "App is damaged and can't be opened"**
- App not notarized
- User can bypass: Right-click → Open

**Windows: "Windows protected your PC"**
- Installer not signed
- User can bypass: More info → Run anyway

**Linux: "Permission denied"**
```bash
chmod +x SOP-AI-Desktop.AppImage
```

## Future Enhancements

### Planned Features

- [ ] Global keyboard shortcuts
- [ ] Custom window positioning/sizing preferences
- [ ] Multiple chat windows (topic-based)
- [ ] Auto-update mechanism
- [ ] Custom themes (dark/light)
- [ ] Offline mode support
- [ ] Chat history persistence
- [ ] Screenshot/clipboard integration
- [ ] Markdown rendering in chat
- [ ] Code syntax highlighting

### Architecture Improvements

- [ ] WebSocket for real-time updates
- [ ] Separate process for httpserver (more isolation)
- [ ] Electron IPC for better window communication
- [ ] Config file for window preferences
- [ ] Logging to file (not just console)

## Related Documentation

- [httpserver Documentation](../httpserver/README.md)
- [sopbackend Architecture](../sopbackend/README.md)
- [Local RAG Design](../../docs/LOCAL_RAG_AUTH_DESIGN.md)
- [Electron Official Docs](https://www.electronjs.org/docs)

## Support

For issues or questions:
- Check [README.md](README.md) in electron-app directory
- Review Electron logs in terminal
- Check httpserver logs for backend issues
- See main SOP Cloud repository for general support
