import { spawn } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const DESKTOP_ROOT = path.resolve(__dirname, '..');

function findInPath(exeName) {
  const pathEnv = process.env.PATH || '';
  const localBin = path.join(DESKTOP_ROOT, 'node_modules', '.bin');
  for (const dir of pathEnv.split(path.delimiter)) {
    if (!dir || path.resolve(dir) === localBin) continue;
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

function resolveElectronBinary() {
  const distBin =
    process.platform === 'win32'
      ? path.join(DESKTOP_ROOT, 'node_modules', 'electron', 'dist', 'electron.exe')
      : process.platform === 'darwin'
        ? path.join(
            DESKTOP_ROOT,
            'node_modules',
            'electron',
            'dist',
            'Electron.app',
            'Contents',
            'MacOS',
            'Electron',
          )
        : path.join(DESKTOP_ROOT, 'node_modules', 'electron', 'dist', 'electron');

  const candidates = [
    process.env.ELECTRON_BIN,
    findInPath(process.platform === 'win32' ? 'electron.exe' : 'electron'),
    distBin,
  ].filter(Boolean);

  for (const c of candidates) {
    try {
      fs.accessSync(c, fs.constants.X_OK);
      return c;
    } catch {
      // continue
    }
  }
  return null;
}

const electronBin = resolveElectronBinary();
if (!electronBin) {
  console.error(
    [
      'Electron binary not found in PATH or node_modules.',
      'On NixOS, run with:',
      '  env -u LD_LIBRARY_PATH nix --extra-experimental-features "nix-command flakes" shell nixpkgs#electron -c pnpm --dir desktop desktop',
    ].join('\n'),
  );
  process.exit(1);
}

const args = [path.join(DESKTOP_ROOT, 'electron', 'main.mjs'), ...process.argv.slice(2)];
if (process.platform === 'linux') {
  args.unshift('--enable-features=WaylandWindowDecorations');
  args.unshift('--ozone-platform-hint=auto');
}

const child = spawn(electronBin, args, {
  cwd: DESKTOP_ROOT,
  stdio: 'inherit',
});

child.on('exit', (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code ?? 0);
});
