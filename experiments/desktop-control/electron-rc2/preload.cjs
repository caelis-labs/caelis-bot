const {contextBridge, ipcRenderer} = require('electron');
contextBridge.exposeInMainWorld('fixture', {
  record: (event, value) => ipcRenderer.send('fixture-event', event, String(value))
});
