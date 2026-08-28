const { app, BrowserWindow } = require('electron');
const path = require('path');

// Start the existing lanDrop server in-process (binds 0.0.0.0 so LAN peers
// can still join via browser at the printed address).
let PORT = 3000;
const http = require('http');
const net = require('net');

function findFreePort(start) {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.listen(start, '0.0.0.0', () => srv.close(() => resolve(start)));
    srv.on('error', () => resolve(start + 1));
  });
}

async function startServer() {
  PORT = await findFreePort(3000);
  process.env.PORT = String(PORT);
  require(path.join(__dirname, '..', 'server.js'));
}

function createWindow() {
  const win = new BrowserWindow({
    width: 1200,
    height: 760,
    autoHideMenuBar: true,
    title: 'lanDrop',
    webPreferences: { contextIsolation: true, nodeIntegration: false },
  });
  win.loadURL(`http://127.0.0.1:${PORT}`);
}

app.whenReady().then(async () => {
  await startServer();
  createWindow();
  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on('window-all-closed', () => app.quit());
