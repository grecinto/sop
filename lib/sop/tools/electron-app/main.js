const { app, BrowserWindow, ipcMain, Menu, Tray, nativeImage } = require('electron');
const { spawn } = require('child_process');
const path = require('path');
const http = require('http');

let httpserverProcess = null;
let mainWindow = null;
let chatWindow = null;
let tray = null;
const PORT = 8080;
const isDev = process.argv.includes('--dev');

// Path to httpserver binary
const getHttpServerPath = () => {
  if (isDev) {
    // In development, use the built binary from tools/httpserver
    return path.join(__dirname, '../httpserver/httpserver');
  } else {
    // In production, use the bundled binary
    return path.join(process.resourcesPath, 'bin/httpserver');
  }
};

// Wait for server to be ready
function waitForServer(url, timeout = 30000) {
  return new Promise((resolve, reject) => {
    const startTime = Date.now();
    
    const checkServer = () => {
      http.get(url, (res) => {
        if (res.statusCode === 200 || res.statusCode === 302) {
          resolve();
        } else {
          retry();
        }
      }).on('error', retry);
    };
    
    const retry = () => {
      if (Date.now() - startTime > timeout) {
        reject(new Error('Server start timeout'));
      } else {
        setTimeout(checkServer, 500);
      }
    };
    
    checkServer();
  });
}

// Start httpserver process
async function startHttpServer() {
  return new Promise((resolve, reject) => {
    const httpserverPath = getHttpServerPath();
    console.log('Starting httpserver:', httpserverPath);
    
    httpserverProcess = spawn(httpserverPath, [
      '--port', PORT.toString(),
      '--production'
    ], {
      stdio: ['ignore', 'pipe', 'pipe']
    });
    
    httpserverProcess.stdout.on('data', (data) => {
      console.log('[httpserver]', data.toString().trim());
    });
    
    httpserverProcess.stderr.on('data', (data) => {
      console.error('[httpserver error]', data.toString().trim());
    });
    
    httpserverProcess.on('error', (error) => {
      console.error('Failed to start httpserver:', error);
      reject(error);
    });
    
    httpserverProcess.on('exit', (code) => {
      console.log('httpserver exited with code:', code);
    });
    
    // Wait for server to be ready
    waitForServer(`http://localhost:${PORT}`)
      .then(resolve)
      .catch(reject);
  });
}

// Create the main browser window (full UI)
function createMainWindow() {
  mainWindow = new BrowserWindow({
    width: 1400,
    height: 900,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      nodeIntegration: false,
      contextIsolation: true
    },
    show: false
  });
  
  mainWindow.loadURL(`http://localhost:${PORT}`);
  
  mainWindow.once('ready-to-show', () => {
    mainWindow.show();
  });
  
  mainWindow.on('closed', () => {
    mainWindow = null;
  });
  
  if (isDev) {
    mainWindow.webContents.openDevTools();
  }
}

// Create the floating chat window (always on top)
function createChatWindow() {
  chatWindow = new BrowserWindow({
    width: 420,
    height: 650,
    alwaysOnTop: true,
    frame: true,
    title: 'SOP AI Chat',
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      nodeIntegration: false,
      contextIsolation: true
    },
    show: false,
    resizable: true,
    minimizable: true,
    maximizable: false,
    fullscreenable: false,
    skipTaskbar: false,
    backgroundColor: '#1e1e1e'
  });
  
  // Load the chat-only route (we'll create this)
  chatWindow.loadURL(`http://localhost:${PORT}/chat`);
  
  chatWindow.once('ready-to-show', () => {
    chatWindow.show();
    
    // Position in bottom-right corner
    const { screen } = require('electron');
    const primaryDisplay = screen.getPrimaryDisplay();
    const { width, height } = primaryDisplay.workAreaSize;
    
    chatWindow.setPosition(
      width - 420 - 20,  // 20px padding from right
      height - 650 - 20  // 20px padding from bottom
    );
  });
  
  chatWindow.on('closed', () => {
    chatWindow = null;
  });
  
  if (isDev) {
    chatWindow.webContents.openDevTools();
  }
}

// Create system tray
function createTray() {
  // Create a simple tray icon (you can replace with actual icon)
  const icon = nativeImage.createFromDataURL(
    'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAABHNCSVQICAgIfAhkiAAAAAlwSFlzAAAAdgAAAHYBTnsmCAAAABl0RVh0U29mdHdhcmUAd3d3Lmlua3NjYXBlLm9yZ5vuPBoAAAFPSURBVDiNpZK9SgNBFIW/2U2yiYmJhY2FhY2FYGMhWFgIFhaCha+QR7CwsLAQLCwECx/AwkKwsBAsfAALwcLCwsJCsLDwB2KyO3Nndm7mzkQpRQjBGMM5x1rLWotSCmstIQRCCKy1WGsJIRBCYIzBWksIgXOOtRZjDCEExhiMMay1rLU45zDGYK0lhEAIgbUWYwzOOZxzOOcIIeCcwzmHtRZrLSEExhicc1hr+Qsi8DzPzMyMTqdDr9ej1+vR7/fp9/u8vr7SbDbxfZ9mswnA4+Mj9Xqd5+dnAGq1GlVVMRgMqKqK6XRKWZYURUFRFJRlSZ7nzM/Ps7S0xPr6OptbW6yvr3N4eMjOzg779+9TFAWz2YyyLCnLkqIomE6nTCYTptMpk8mEqqoYjUYMh0OGwyGj0YjJZMJ4PGY8HjMej5lOp0ynU6qqYjQaMRqNGI1GVFXFZDJhMpkwmUz4BhAC1P0SFVAAAAAElFTkSuQmCC'
  );
  
  tray = new Tray(icon);
  
  const contextMenu = Menu.buildFromTemplate([
    {
      label: 'Show Main Window',
      click: () => {
        if (mainWindow) {
          mainWindow.show();
        } else {
          createMainWindow();
        }
      }
    },
    {
      label: 'Toggle Chat Window',
      click: () => {
        if (chatWindow) {
          if (chatWindow.isVisible()) {
            chatWindow.hide();
          } else {
            chatWindow.show();
          }
        } else {
          createChatWindow();
        }
      }
    },
    { type: 'separator' },
    {
      label: 'Quit',
      click: () => {
        app.quit();
      }
    }
  ]);
  
  tray.setToolTip('SOP AI Desktop');
  tray.setContextMenu(contextMenu);
  
  // Double-click tray to toggle chat window
  tray.on('double-click', () => {
    if (chatWindow) {
      if (chatWindow.isVisible()) {
        chatWindow.hide();
      } else {
        chatWindow.show();
      }
    } else {
      createChatWindow();
    }
  });
}

// IPC handlers
ipcMain.on('open-chat-window', () => {
  if (!chatWindow) {
    createChatWindow();
  } else {
    chatWindow.show();
    chatWindow.focus();
  }
});

ipcMain.on('close-chat-window', () => {
  if (chatWindow) {
    chatWindow.close();
  }
});

// App lifecycle
app.whenReady().then(async () => {
  try {
    console.log('Starting SOP AI Desktop...');
    
    // Start the Go httpserver backend
    await startHttpServer();
    console.log('httpserver ready at http://localhost:' + PORT);
    
    // Create windows
    createMainWindow();
    createChatWindow();
    createTray();
    
  } catch (error) {
    console.error('Failed to start application:', error);
    app.quit();
  }
});

app.on('window-all-closed', () => {
  // Don't quit on window close - keep tray icon
  // User must explicitly quit from tray menu
});

app.on('activate', () => {
  if (mainWindow === null) {
    createMainWindow();
  }
});

app.on('before-quit', () => {
  // Kill httpserver process
  if (httpserverProcess) {
    console.log('Stopping httpserver...');
    httpserverProcess.kill();
  }
});
