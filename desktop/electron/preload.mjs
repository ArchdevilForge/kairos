import { contextBridge, ipcRenderer } from 'electron';

contextBridge.exposeInMainWorld('kairosDesktop', {
  getConfig: () => ipcRenderer.invoke('kairos:get-config'),
  syncNow: () => ipcRenderer.invoke('kairos:sync-now'),
  notify: (payload) => ipcRenderer.send('kairos:notify', payload),
  onTriggerSync: (cb) => {
    const handler = () => cb();
    ipcRenderer.on('kairos:trigger-sync', handler);
    return () => ipcRenderer.removeListener('kairos:trigger-sync', handler);
  },
  onFocusSymbol: (cb) => {
    const handler = (_evt, symbol) => cb(symbol);
    ipcRenderer.on('kairos:focus-symbol', handler);
    return () => ipcRenderer.removeListener('kairos:focus-symbol', handler);
  },
});
