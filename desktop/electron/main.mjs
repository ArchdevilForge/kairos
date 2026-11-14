import {
  app,
  BrowserWindow,
  Menu,
  Notification,
  Tray,
  ipcMain,
  nativeImage,
  powerMonitor,
} from 'electron';
import { execFileSync, spawn } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import readline from 'node:readline';
import { fileURLToPath } from 'node:url';
import zlib from 'node:zlib';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const DESKTOP_ROOT = path.resolve(__dirname, '..');
const REPO_ROOT = path.resolve(DESKTOP_ROOT, '..');

if (process.platform === 'linux') {
  app.commandLine.appendSwitch('ozone-platform-hint', 'auto');
  app.commandLine.appendSwitch('enable-features', 'WaylandWindowDecorations');
}

let mainWindow = null;
let tray = null;
let daemonProc = null;
let tempBuiltBin = null;
let isQuitting = false;
let deskConfig = null;
let wsClient = null;
let wsReconnectTimer = null;
let cleanupDone = false;
let lastPulseState = '';
let lastPulseDir = '';
const notifiedIds = new Set();

const PULSE_NOTIFY_TITLE = {
  IMPULSE_UP: '大盘脉冲上攻',
  IMPULSE_DOWN: '大盘脉冲下杀',
  TRENDING_UP: '大盘延续上攻',
  TRENDING_DOWN: '大盘延续下跌',
  STRESS_UP: '大盘极端上攻',
  STRESS_DOWN: '大盘极端下跌',
};

function pulseDirection(state) {
  if (state === 'IMPULSE_UP' || state === 'TRENDING_UP' || state === 'STRESS_UP') {
    return 'up';
  }
  if (state === 'IMPULSE_DOWN' || state === 'TRENDING_DOWN' || state === 'STRESS_DOWN') {
    return 'down';
  }
  return '';
}

app.setName('Kairos Desk');

function crc32(buf) {
  let c = 0xffffffff;
  for (let i = 0; i < buf.length; i += 1) {
    c ^= buf[i];
    for (let k = 0; k < 8; k += 1) {
      c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    }
  }
  return (c ^ 0xffffffff) >>> 0;
}

function pngChunk(type, data) {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length, 0);
  const typeBuf = Buffer.from(type, 'ascii');
  const crcBuf = Buffer.alloc(4);
  crcBuf.writeUInt32BE(crc32(Buffer.concat([typeBuf, data])), 0);
  return Buffer.concat([len, typeBuf, data, crcBuf]);
}

function createTrayIcon() {
  const size = 24;
  const raw = Buffer.alloc((size * 4 + 1) * size);
  const cx = (size - 1) / 2;
  const cy = (size - 1) / 2;

  for (let y = 0; y < size; y += 1) {
    const rowStart = y * (size * 4 + 1);
    raw[rowStart] = 0;
    for (let x = 0; x < size; x += 1) {
      const idx = rowStart + 1 + x * 4;
      const dx = Math.abs(x - cx);
      const dy = Math.abs(y - cy);
      const inCircle = dx * dx + dy * dy <= 10.8 * 10.8;
      const inDiamond = dx + dy <= 5.6 && dx + dy >= 2.6;
      const inCenter = dx <= 1.2 && dy <= 1.2;

      if (!inCircle) {
        raw[idx] = 0;
        raw[idx + 1] = 0;
        raw[idx + 2] = 0;
        raw[idx + 3] = 0;
      } else if (inDiamond || inCenter) {
        // Warm terracotta #D97757
        raw[idx] = 0xd9;
        raw[idx + 1] = 0x77;
        raw[idx + 2] = 0x57;
        raw[idx + 3] = 0xff;
      } else {
        // Warm obsidian #141413
        raw[idx] = 0x14;
        raw[idx + 1] = 0x14;
        raw[idx + 2] = 0x13;
        raw[idx + 3] = 0xff;
      }
    }
  }

  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = 6; // RGBA
  ihdr[10] = 0;
  ihdr[11] = 0;
  ihdr[12] = 0;

  const compressed = zlib.deflateSync(raw);
  const png = Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    pngChunk('IHDR', ihdr),
    pngChunk('IDAT', compressed),
    pngChunk('IEND', Buffer.alloc(0)),
  ]);

  return nativeImage.createFromBuffer(png);
}

function findInPath(exeName) {
  const pathEnv = process.env.PATH || '';
  for (const dir of pathEnv.split(path.delimiter)) {
    if (!dir) continue;
    const candidate = path.join(dir, exeName);
    try {
      fs.accessSync(candidate, fs.constants.X_OK);
      return candidate;
    } catch {
      // continue
    }
  }
  return null;
}

function resolveConfigPath() {
  if (process.env.KAIROS_CONFIG) {
    return process.env.KAIROS_CONFIG;
  }
  const candidates = [
    path.join(REPO_ROOT, 'config', 'config.yaml'),
    path.join(os.homedir(), '.config', 'kairos', 'config.yaml'),
  ];
  for (const c of candidates) {
    if (fs.existsSync(c)) return c;
  }
  return path.join(os.homedir(), '.config', 'kairos', 'config.yaml');
}

function resolveKairosdBinary() {
  const exeName = process.platform === 'win32' ? 'kairosd.exe' : 'kairosd';
  const candidates = [
    process.env.KAIROSD_BIN,
    process.resourcesPath ? path.join(process.resourcesPath, exeName) : null,
    path.join(REPO_ROOT, exeName),
    path.join(REPO_ROOT, 'bin', exeName),
    findInPath(exeName),
  ].filter(Boolean);

  for (const c of candidates) {
    try {
      fs.accessSync(c, fs.constants.X_OK);
      return c;
    } catch {
      // continue
    }
  }

  const goMod = path.join(REPO_ROOT, 'go.mod');
  const goBin = findInPath(process.platform === 'win32' ? 'go.exe' : 'go');
  if (fs.existsSync(goMod) && goBin) {
    const outBin = path.join(
      os.tmpdir(),
      `kairosd-desk-${process.pid}${process.platform === 'win32' ? '.exe' : ''}`,
    );
    execFileSync(goBin, ['build', '-o', outBin, './cmd/kairosd'], {
      cwd: REPO_ROOT,
      stdio: 'inherit',
    });
    tempBuiltBin = outBin;
    return outBin;
  }

  throw new Error(
    'Unable to locate kairosd binary. Build it with `go build -o kairosd ./cmd/kairosd` or set KAIROSD_BIN.',
  );
}

function startDaemon() {
  if (process.env.KAIROS_DESK_URL && process.env.KAIROS_DESK_TOKEN) {
    const url = process.env.KAIROS_DESK_URL.replace(/\/+$/, '');
    const wsUrl =
      process.env.KAIROS_DESK_WS_URL ||
      `${url.replace(/^http/i, 'ws')}/api/v1/ws`;
    return Promise.resolve({
      url,
      wsUrl,
      token: process.env.KAIROS_DESK_TOKEN,
    });
  }

  const binPath = resolveKairosdBinary();
  const cfgPath = resolveConfigPath();

  return new Promise((resolve, reject) => {
    const proc = spawn(
      binPath,
      ['--desk-listen', '127.0.0.1:0', '--config', cfgPath],
      {
        cwd: REPO_ROOT,
        stdio: ['ignore', 'pipe', 'pipe'],
      },
    );
    daemonProc = proc;

    let settled = false;
    const timeout = setTimeout(() => {
      if (!settled) {
        settled = true;
        reject(new Error('Timed out waiting for KAIROSD_DESK_READY'));
      }
    }, 20_000);

    const rl = readline.createInterface({ input: proc.stdout });
    rl.on('line', (line) => {
      const trimmed = line.trim();
      if (!settled && trimmed.startsWith('KAIROSD_DESK_READY ')) {
        try {
          const raw = trimmed.slice('KAIROSD_DESK_READY '.length);
          const parsed = JSON.parse(raw);
          settled = true;
          clearTimeout(timeout);
          resolve({
            url: String(parsed.url || '').replace(/\/+$/, ''),
            wsUrl: String(parsed.ws_url || parsed.wsUrl || ''),
            token: String(parsed.token || ''),
          });
        } catch (err) {
          settled = true;
          clearTimeout(timeout);
          reject(new Error(`Invalid KAIROSD_DESK_READY JSON: ${err.message}`));
        }
      } else {
        console.log(`[kairosd] ${line}`);
      }
    });

    proc.stderr.on('data', (chunk) => {
      process.stderr.write(`[kairosd] ${chunk}`);
    });

    proc.on('exit', (code, signal) => {
      daemonProc = null;
      if (!settled) {
        settled = true;
        clearTimeout(timeout);
        reject(new Error(`kairosd exited early (code=${code}, signal=${signal})`));
      } else if (!isQuitting) {
        console.error(`kairosd terminated unexpectedly (code=${code}, signal=${signal})`);
      }
    });
  });
}

async function callDaemon(pathname, method = 'POST') {
  if (!deskConfig?.url || !deskConfig?.token) return;
  try {
    await fetch(`${deskConfig.url}${pathname}`, {
      method,
      headers: {
        Authorization: `Bearer ${deskConfig.token}`,
      },
    });
  } catch (err) {
    console.error(`[kairos-desk] ${method} ${pathname} failed:`, err.message);
  }
}

function showNotification(id, title, body, symbol) {
  if (!Notification.isSupported()) return;
  if (id) {
    if (notifiedIds.has(id)) return;
    notifiedIds.add(id);
    if (notifiedIds.size > 250) {
      const oldest = notifiedIds.values().next().value;
      if (oldest) notifiedIds.delete(oldest);
    }
  }
  const n = new Notification({
    title,
    body,
    silent: false,
  });
  n.on('click', () => {
    showMainWindow();
    if (symbol && mainWindow && !mainWindow.isDestroyed()) {
      mainWindow.webContents.send('kairos:focus-symbol', symbol);
    }
  });
  n.show();
}

function connectDaemonNotifications() {
  if (!deskConfig?.wsUrl || !deskConfig?.token || typeof WebSocket === 'undefined') {
    return;
  }
  if (wsClient || isQuitting) return;

  const sep = deskConfig.wsUrl.includes('?') ? '&' : '?';
  const url = `${deskConfig.wsUrl}${sep}token=${encodeURIComponent(deskConfig.token)}`;

  try {
    const ws = new WebSocket(url);
    wsClient = ws;

    ws.onopen = () => {
      ws.send(JSON.stringify({ type: 'subscribe', symbol: '', tfs: [] }));
    };

    ws.onmessage = (msg) => {
      if (typeof msg.data !== 'string') return;
      try {
        const evt = JSON.parse(msg.data);
        if (evt.type === 'pulse') {
          const nextState = evt.pulse?.state || '';
          const nextDir = pulseDirection(nextState);
          const prevState = lastPulseState;
          const prevDir = lastPulseDir;
          const shouldNotify =
            nextDir !== '' &&
            (prevState === '' ||
              prevState === 'QUIET' ||
              prevState === 'DECAY' ||
              prevDir !== nextDir);

          lastPulseState = nextState;
          lastPulseDir = nextDir;

          if (shouldNotify) {
            const breadth =
              nextDir === 'down'
                ? Number(evt.pulse?.down_breadth_60s || 0)
                : Number(evt.pulse?.up_breadth_60s || 0);
            const med60 = Number(evt.pulse?.median_return_60s || 0);
            const rawMovers =
              nextDir === 'down'
                ? evt.pulse?.snapshot?.laggards
                : evt.pulse?.snapshot?.leaders;
            const movers = Array.isArray(rawMovers)
              ? rawMovers
                  .filter((m) => m?.symbol && m.symbol.toUpperCase() !== 'MARKET')
                  .slice(0, 3)
              : [];
            const topSym = movers.length > 0 ? movers[0].symbol : undefined;
            const topLabels = movers
              .map((m) => {
                const coin = String(m.symbol)
                  .replace(/\/USDT(:USDT)?$/i, '')
                  .replace(/:USDT$/i, '');
                const ret = Number(m.return_pct || 0);
                return `${coin} ${ret >= 0 ? '+' : ''}${ret.toFixed(2)}%`;
              })
              .join(' · ');
            const summary = `60s 中位 ${med60 >= 0 ? '+' : ''}${med60.toFixed(2)}% · 广度 ${(breadth * 100).toFixed(0)}%`;
            showNotification(
              `pulse:${nextState}:${evt.pulse?.updated_at || Date.now()}`,
              `MarketPulse · ${PULSE_NOTIFY_TITLE[nextState] || nextState}`,
              topLabels ? `${summary} | ${topLabels}` : summary,
              topSym,
            );
          }
        } else if (evt.type === 'ticket_updated' && evt.bundle?.ticket) {
          const t = evt.bundle.ticket;
          if (t.status === 'open') {
            showNotification(
              `ticket:${t.id}:${t.status}`,
              `Decision Ticket · ${t.symbol}`,
              `${t.direction.toUpperCase()} Grade ${t.grade} · ${t.playbook_id}`,
              t.symbol,
            );
          }
        } else if (evt.type === 'notice' && evt.notice) {
          const n = evt.notice;
          if (n.delivery_status !== 'suppressed') {
            showNotification(
              `notice:${n.notice_id}`,
              `Notice · ${n.symbol}`,
              n.trigger_facts?.fact_summary || n.env?.box_state || 'Scene detected',
              n.symbol,
            );
          }
        }
      } catch {
        // ignore malformed frame
      }
    };

    ws.onclose = () => {
      wsClient = null;
      if (!isQuitting && !wsReconnectTimer) {
        wsReconnectTimer = setTimeout(() => {
          wsReconnectTimer = null;
          connectDaemonNotifications();
        }, 5_000);
      }
    };

    ws.onerror = () => {
      ws.close();
    };
  } catch {
    // ignore WS startup failure
  }
}

function showMainWindow() {
  if (!mainWindow || mainWindow.isDestroyed()) {
    createMainWindow();
    return;
  }
  if (mainWindow.isMinimized()) {
    mainWindow.restore();
  }
  mainWindow.show();
  mainWindow.focus();
}

function createMainWindow() {
  mainWindow = new BrowserWindow({
    width: 1480,
    height: 920,
    minWidth: 1120,
    minHeight: 700,
    title: 'Kairos Desk',
    backgroundColor: '#141413',
    autoHideMenuBar: true,
    show: false,
    webPreferences: {
      preload: path.join(__dirname, 'preload.mjs'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: false,
    },
  });
  mainWindow.setMenuBarVisibility(false);

  mainWindow.once('ready-to-show', () => {
    mainWindow?.show();
  });

  mainWindow.on('close', (event) => {
    if (!isQuitting) {
      event.preventDefault();
      mainWindow?.hide();
    }
  });

  const devUrl = process.env.KAIROS_UI_DEV_URL;
  if (devUrl) {
    mainWindow.loadURL(devUrl);
  } else {
    mainWindow.loadFile(path.join(DESKTOP_ROOT, 'dist', 'index.html'));
  }
}

function createTray() {
  const icon = createTrayIcon();
  tray = new Tray(icon);
  tray.setToolTip('Kairos Desk');

  const menu = Menu.buildFromTemplate([
    {
      label: 'Open Kairos Desk',
      click: () => showMainWindow(),
    },
    {
      label: 'Sync Account Now',
      click: async () => {
        await callDaemon('/api/v1/account/sync', 'POST');
        if (mainWindow && !mainWindow.isDestroyed()) {
          mainWindow.webContents.send('kairos:trigger-sync');
        }
      },
    },
    { type: 'separator' },
    {
      label: 'Quit Kairos & Stop Daemon',
      click: () => {
        isQuitting = true;
        app.quit();
      },
    },
  ]);

  tray.setContextMenu(menu);
  tray.on('double-click', () => showMainWindow());
}

async function stopDaemonGracefully() {
  if (wsReconnectTimer) {
    clearTimeout(wsReconnectTimer);
    wsReconnectTimer = null;
  }
  if (wsClient) {
    try {
      wsClient.close();
    } catch {
      // ignore
    }
    wsClient = null;
  }
  if (daemonProc) {
    const proc = daemonProc;
    daemonProc = null;
    await new Promise((resolve) => {
      const timer = setTimeout(() => {
        try {
          proc.kill('SIGKILL');
        } catch {
          // ignore
        }
        resolve();
      }, 3_000);
      proc.once('exit', () => {
        clearTimeout(timer);
        resolve();
      });
      try {
        proc.kill('SIGTERM');
      } catch {
        clearTimeout(timer);
        resolve();
      }
    });
  }
  if (tempBuiltBin && fs.existsSync(tempBuiltBin)) {
    try {
      fs.unlinkSync(tempBuiltBin);
    } catch {
      // ignore
    }
    tempBuiltBin = null;
  }
}

ipcMain.handle('kairos:get-config', () => deskConfig);
ipcMain.handle('kairos:sync-now', async () => {
  await callDaemon('/api/v1/account/sync', 'POST');
});
ipcMain.on('kairos:notify', (_evt, payload) => {
  if (!payload || !payload.title) return;
  showNotification(payload.id, payload.title, payload.body || '', payload.symbol);
});

const singleLock = app.requestSingleInstanceLock();
if (!singleLock) {
  app.quit();
} else {
  app.on('second-instance', () => showMainWindow());

  app.whenReady().then(async () => {
    try {
      deskConfig = await startDaemon();
    } catch (err) {
      console.error('[kairos-desk] failed to start kairosd:', err);
      app.exit(1);
      return;
    }

    Menu.setApplicationMenu(null);
    createTray();
    createMainWindow();
    connectDaemonNotifications();

    powerMonitor.on('resume', () => {
      void callDaemon('/api/v1/system/wake', 'POST');
    });

    app.on('activate', () => {
      if (BrowserWindow.getAllWindows().length === 0) {
        createMainWindow();
      } else {
        showMainWindow();
      }
    });
  });

  app.on('before-quit', async (event) => {
    if (cleanupDone) return;
    event.preventDefault();
    cleanupDone = true;
    isQuitting = true;
    await stopDaemonGracefully();
    app.exit(0);
  });

  app.on('window-all-closed', () => {
    // Keep alive in system tray until explicit quit.
  });
}
