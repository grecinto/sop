const { contextBridge, ipcRenderer } = require('electron');

// Expose protected methods that allow the renderer process to use
// the ipcRenderer without exposing the entire object
contextBridge.exposeInMainWorld('electronAPI', {
  openChatWindow: () => ipcRenderer.send('open-chat-window'),
  closeChatWindow: () => ipcRenderer.send('close-chat-window'),
  platform: process.platform
});
