// Disposable Electron window mirroring Desktop World's full POC context menu.
const {app, BrowserWindow, ipcMain} = require('electron');
const fs = require('node:fs');
const path = require('node:path');

const config = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
app.setPath('userData', config.profile);
app.commandLine.appendSwitch('force-renderer-accessibility');
function record(event, value = '') {
  fs.appendFileSync(config.log, JSON.stringify({event, value: String(value)}) + '\n');
}
app.whenReady().then(() => {
  app.setAccessibilitySupportEnabled(true);
  const win = new BrowserWindow({
    x: 180, y: 200, width: 600, height: 650, title: config.title,
    webPreferences: {
      contextIsolation: true, nodeIntegration: false,
      preload: path.join(__dirname, 'preload.cjs'),
    },
  });
  ipcMain.on('fixture-event', (_event, event, value) => record(event, value));
  win.webContents.on('dom-ready', () => record('ready', config.title));
  win.loadFile(path.join(__dirname, 'index.html'), {query: {title: config.title}});
});
app.on('window-all-closed', () => app.quit());
