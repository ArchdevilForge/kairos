import React, { useCallback, useEffect, useRef, useState } from 'react';
import { AlertTriangle, RefreshCw } from 'lucide-react';
import { CommandPalette } from './components/CommandPalette';
import { SettingsModal } from './components/SettingsModal';
import { TopStatusBar, type WorkspaceTab } from './components/TopStatusBar';
import { DeskClient } from './services/deskClient';
import type {
  AccountSyncResponse,
  AnomalyItem,
  BootstrapPayload,
  MarketState,
  Timeframe,
  UpdateSettingsRequest,
  WsEvent,
} from './types/kairos';
import { ReviewWorkspace } from './views/ReviewWorkspace';
import { StatsWorkspace } from './views/StatsWorkspace';
import { WatchWorkspace } from './views/WatchWorkspace';

const THEME_STORAGE_KEY = 'kairos.desk.theme';
const CHIME_STORAGE_KEY = 'kairos.desk.chime';
const SYMBOL_STORAGE_KEY = 'kairos.desk.symbol';

function sanitizeSymbol(
  raw: string | null | undefined,
  fallback = 'BTC/USDT:USDT',
): string {
  const trimmed = (raw ?? '').trim();
  const upper = trimmed.toUpperCase();
  if (!trimmed || upper === 'MARKET' || !trimmed.includes('/')) {
    return fallback;
  }
  return trimmed;
}

function pulseDirection(state: MarketState | string | null | undefined): 'up' | 'down' | '' {
  if (state === 'IMPULSE_UP' || state === 'TRENDING_UP' || state === 'STRESS_UP') {
    return 'up';
  }
  if (state === 'IMPULSE_DOWN' || state === 'TRENDING_DOWN' || state === 'STRESS_DOWN') {
    return 'down';
  }
  return '';
}

function anomalyKey(a: AnomalyItem): string {
  return a.event_id ? `id:${a.event_id}` : `${a.symbol}|${a.event_type}|${a.timestamp}`;
}

function rememberKey(set: Set<string>, key: string, max = 300): boolean {
  if (set.has(key)) return false;
  set.add(key);
  if (set.size > max) {
    const oldest = set.values().next().value;
    if (oldest !== undefined) set.delete(oldest);
  }
  return true;
}

function readInitialTheme(): 'dark' | 'light' {
  try {
    const saved = localStorage.getItem(THEME_STORAGE_KEY);
    return saved === 'light' ? 'light' : 'dark';
  } catch {
    return 'dark';
  }
}

function readInitialChime(): boolean {
  try {
    const saved = localStorage.getItem(CHIME_STORAGE_KEY);
    return saved !== 'false';
  } catch {
    return true;
  }
}

function playSoftChime(audioCtxRef: React.MutableRefObject<AudioContext | null>): void {
  if (typeof window === 'undefined') return;
  const Ctor =
    window.AudioContext ||
    (window as unknown as { webkitAudioContext?: typeof AudioContext })
      .webkitAudioContext;
  if (!Ctor) return;
  try {
    if (!audioCtxRef.current) {
      audioCtxRef.current = new Ctor();
    }
    const ctx = audioCtxRef.current;
    if (ctx.state === 'suspended') {
      void ctx.resume();
    }
    const now = ctx.currentTime;

    const notes = [587.33, 880];
    notes.forEach((freq, idx) => {
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = 'sine';
      osc.frequency.setValueAtTime(freq, now + idx * 0.09);

      gain.gain.setValueAtTime(0.0001, now + idx * 0.09);
      gain.gain.exponentialRampToValueAtTime(0.045, now + idx * 0.09 + 0.015);
      gain.gain.exponentialRampToValueAtTime(0.0001, now + idx * 0.09 + 0.32);

      osc.connect(gain);
      gain.connect(ctx.destination);
      osc.start(now + idx * 0.09);
      osc.stop(now + idx * 0.09 + 0.34);
    });
  } catch {
    // Ignore audio context autoplay restrictions until user interacts.
  }
}

export function App() {
  const [client, setClient] = useState<DeskClient | null>(null);
  const [bootstrap, setBootstrap] = useState<BootstrapPayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [connError, setConnError] = useState<string | null>(null);
  const [wsConnected, setWsConnected] = useState(false);

  const [activeView, setActiveView] = useState<WorkspaceTab>('watch');
  const [activeSymbol, setActiveSymbol] = useState<string>(() => {
    try {
      return sanitizeSymbol(localStorage.getItem(SYMBOL_STORAGE_KEY));
    } catch {
      return 'BTC/USDT:USDT';
    }
  });
  const [splitMode, setSplitMode] = useState(false);
  const [activeTf, setActiveTf] = useState<Timeframe>('15m');

  const [reviewTarget, setReviewTarget] = useState<{
    symbol: string;
    cursorTs?: number;
    ticketId?: string;
  }>({ symbol: 'BTC/USDT:USDT' });

  const [theme, setTheme] = useState<'dark' | 'light'>(readInitialTheme);
  const [chimeEnabled, setChimeEnabled] = useState<boolean>(readInitialChime);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [syncingAccount, setSyncingAccount] = useState(false);
  const [manualToken, setManualToken] = useState('');
  const [manualApiUrl, setManualApiUrl] = useState('');

  const audioCtxRef = useRef<AudioContext | null>(null);
  const chimeEnabledRef = useRef(chimeEnabled);
  chimeEnabledRef.current = chimeEnabled;
  const lastPulseStateRef = useRef<string>('');
  const lastQuietPulseUpdateRef = useRef<number>(0);
  const seenAnomalyKeysRef = useRef<Set<string>>(new Set());
  const chimedEventIdsRef = useRef<Set<string>>(new Set());

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    try {
      localStorage.setItem(THEME_STORAGE_KEY, theme);
    } catch {
      // ignore storage errors
    }
  }, [theme]);

  useEffect(() => {
    try {
      localStorage.setItem(CHIME_STORAGE_KEY, String(chimeEnabled));
    } catch {
      // ignore storage errors
    }
  }, [chimeEnabled]);

  useEffect(() => {
    try {
      localStorage.setItem(SYMBOL_STORAGE_KEY, activeSymbol);
    } catch {
      // ignore storage errors
    }
  }, [activeSymbol]);

  const selectTradeableSymbol = useCallback((sym: string) => {
    setActiveSymbol((prev) => sanitizeSymbol(sym, prev || 'BTC/USDT:USDT'));
  }, []);

  const refreshBootstrap = useCallback(async (targetClient?: DeskClient) => {
    const active = targetClient ?? client;
    if (!active) return;
    try {
      const data = await active.bootstrap();
      const visibleNotices = (data.notices ?? []).filter(
        (n) => n.delivery_status !== 'suppressed',
      );
      for (const a of data.anomalies ?? []) {
        const key = anomalyKey(a);
        rememberKey(seenAnomalyKeysRef.current, key);
        rememberKey(chimedEventIdsRef.current, a.event_id || key);
      }
      for (const n of visibleNotices) {
        rememberKey(chimedEventIdsRef.current, `notice:${n.notice_id}`);
        if (n.trigger_facts?.event_id) {
          rememberKey(chimedEventIdsRef.current, n.trigger_facts.event_id);
        }
      }
      if (!lastPulseStateRef.current && data.status?.pulse?.state) {
        lastPulseStateRef.current = data.status.pulse.state;
      }
      setBootstrap({ ...data, notices: visibleNotices });
      setConnError(null);
      setActiveSymbol((prev) => {
        const clean = sanitizeSymbol(prev, '');
        if (clean) return clean;
        return sanitizeSymbol(data.status.symbols[0], 'BTC/USDT:USDT');
      });
    } catch (err) {
      setConnError(
        err instanceof Error ? err.message : 'Unable to reach kairosd API',
      );
    }
  }, [client]);

  useEffect(() => {
    let cancelled = false;
    let currentClient: DeskClient | null = null;
    let unsubWs: (() => void) | null = null;
    let unsubConn: (() => void) | null = null;

    async function init() {
      setLoading(true);
      try {
        const c = await DeskClient.createFromEnvironment();
        if (cancelled) return;
        currentClient = c;
        setClient(c);

        unsubConn = c.onConnectionChange((connected) => {
          if (!cancelled) setWsConnected(connected);
        });

        unsubWs = c.onWsEvent((evt: WsEvent) => {
          if (cancelled) return;
          if (evt.type === 'pulse') {
            const nextState = evt.pulse?.state ?? '';
            const nextDir = pulseDirection(nextState);
            const prevState = lastPulseStateRef.current;
            const prevDir = pulseDirection(prevState);
            const shouldChime =
              nextDir !== '' &&
              (prevState === '' ||
                prevState === 'QUIET' ||
                prevState === 'DECAY' ||
                prevDir !== nextDir);

            lastPulseStateRef.current = nextState;

            const now = Date.now();
            const skipQuietUpdate =
              nextState === 'QUIET' &&
              prevState === 'QUIET' &&
              now - lastQuietPulseUpdateRef.current < 15_000;

            if (!skipQuietUpdate) {
              if (nextState === 'QUIET') {
                lastQuietPulseUpdateRef.current = now;
              }
              setBootstrap((prev) =>
                prev
                  ? {
                      ...prev,
                      status: {
                        ...prev.status,
                        pulse: evt.pulse,
                        season_regime:
                          evt.season_regime !== undefined
                            ? evt.season_regime
                            : prev.status.season_regime,
                      },
                    }
                  : prev,
              );
            }
            if (shouldChime && chimeEnabledRef.current) {
              playSoftChime(audioCtxRef);
            }
          } else if (evt.type === 'anomaly') {
            const key = anomalyKey(evt.anomaly);
            const isNew = rememberKey(seenAnomalyKeysRef.current, key);
            setBootstrap((prev) => {
              if (!prev) return prev;
              if (prev.anomalies.some((a) => anomalyKey(a) === key)) {
                return prev;
              }
              return {
                ...prev,
                anomalies: [evt.anomaly, ...prev.anomalies].slice(0, 200),
              };
            });
            if (isNew && !evt.anomaly.event_type.startsWith('market_')) {
              const eid = evt.anomaly.event_id || key;
              if (rememberKey(chimedEventIdsRef.current, eid) && chimeEnabledRef.current) {
                playSoftChime(audioCtxRef);
              }
            }
          } else if (evt.type === 'notice') {
            if (evt.notice?.delivery_status === 'suppressed') {
              return;
            }
            setBootstrap((prev) => {
              if (!prev) return prev;
              const exists = prev.notices.some(
                (n) => n.notice_id === evt.notice.notice_id,
              );
              const nextNotices = exists
                ? prev.notices.map((n) =>
                    n.notice_id === evt.notice.notice_id ? evt.notice : n,
                  )
                : [evt.notice, ...prev.notices].slice(0, 100);
              return { ...prev, notices: nextNotices };
            });
            const triggerId = evt.notice.trigger_facts?.event_id;
            const alreadyChimedTrigger = triggerId
              ? chimedEventIdsRef.current.has(triggerId)
              : false;
            const firstNoticeChime = rememberKey(
              chimedEventIdsRef.current,
              `notice:${evt.notice.notice_id}`,
            );
            if (triggerId) {
              rememberKey(chimedEventIdsRef.current, triggerId);
            }
            if (firstNoticeChime && !alreadyChimedTrigger && chimeEnabledRef.current) {
              playSoftChime(audioCtxRef);
            }
          } else if (evt.type === 'ticket_updated') {
            setBootstrap((prev) => {
              if (!prev) return prev;
              const idx = prev.tickets.findIndex(
                (b) => b.ticket.id === evt.bundle.ticket.id,
              );
              const nextTickets =
                idx >= 0
                  ? prev.tickets.map((b, i) => (i === idx ? evt.bundle : b))
                  : [evt.bundle, ...prev.tickets];
              return { ...prev, tickets: nextTickets };
            });
            if (evt.bundle.ticket.status === 'open' && chimeEnabledRef.current) {
              playSoftChime(audioCtxRef);
            }
          } else if (evt.type === 'account_updated') {
            setBootstrap((prev) =>
              prev
                ? {
                    ...prev,
                    status: { ...prev.status, account: evt.account },
                    open_positions: evt.open_positions ?? prev.open_positions,
                  }
                : prev,
            );
          }
        });

        await refreshBootstrap(c);
        c.connectWs();
      } catch (err) {
        if (!cancelled) {
          setConnError(
            err instanceof Error ? err.message : 'Initialization failed',
          );
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    void init();

    return () => {
      cancelled = true;
      unsubWs?.();
      unsubConn?.();
      currentClient?.disconnectWs();
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const handleSyncAccount = useCallback(
    async (opts?: { rethrow?: boolean }): Promise<AccountSyncResponse | undefined> => {
      if (!client || syncingAccount) return undefined;
      setSyncingAccount(true);
      try {
        const res = await client.syncAccount();
        await refreshBootstrap(client);
        return res;
      } catch (err) {
        if (opts?.rethrow) {
          throw err;
        }
        setConnError(
          err instanceof Error ? err.message : 'Account sync failed',
        );
        return undefined;
      } finally {
        setSyncingAccount(false);
      }
    },
    [client, refreshBootstrap, syncingAccount],
  );

  useEffect(() => {
    const unsubSync = window.kairosDesktop?.onTriggerSync?.(() => {
      void handleSyncAccount();
    });
    const unsubFocus = window.kairosDesktop?.onFocusSymbol?.((symbol) => {
      if (symbol) {
        selectTradeableSymbol(symbol);
        setActiveView('watch');
      }
    });
    return () => {
      unsubSync?.();
      unsubFocus?.();
    };
  }, [handleSyncAccount, selectTradeableSymbol]);

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setPaletteOpen((prev) => !prev);
      } else if (e.key === 'Escape') {
        if (paletteOpen) {
          e.preventDefault();
          setPaletteOpen(false);
        } else if (settingsOpen) {
          e.preventDefault();
          setSettingsOpen(false);
        }
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [paletteOpen, settingsOpen]);

  const handleOpenInReview = useCallback(
    (symbol: string, cursorTs?: number, ticketId?: string) => {
      const clean = sanitizeSymbol(symbol);
      setActiveSymbol(clean);
      setReviewTarget({ symbol: clean, cursorTs, ticketId });
      setActiveView('review');
    },
    [],
  );

  const handleSaveSettings = useCallback(
    async (
      req: UpdateSettingsRequest,
      syncAfter: boolean,
    ): Promise<AccountSyncResponse | undefined> => {
      if (!client) return undefined;
      await client.updateSettings(req);
      let syncRes: AccountSyncResponse | undefined;
      if (syncAfter) {
        syncRes = await client.syncAccount();
      }
      await refreshBootstrap(client);
      return syncRes;
    },
    [client, refreshBootstrap],
  );

  const handleToggleTheme = useCallback(() => {
    setTheme((prev) => (prev === 'dark' ? 'light' : 'dark'));
  }, []);

  const handleToggleSplit = useCallback(() => {
    setSplitMode((prev) => !prev);
  }, []);

  const handleOpenPalette = useCallback(() => {
    setPaletteOpen(true);
  }, []);

  const handleClosePalette = useCallback(() => {
    setPaletteOpen(false);
  }, []);

  const handleOpenSettings = useCallback(() => {
    setSettingsOpen(true);
  }, []);

  const handleCloseSettings = useCallback(() => {
    setSettingsOpen(false);
  }, []);

  const handleToggleChime = useCallback(() => {
    setChimeEnabled((prev) => !prev);
  }, []);

  const handleTriggerSync = useCallback(() => {
    void handleSyncAccount();
  }, [handleSyncAccount]);

  const handleManualConnect = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!client) return;
    try {
      if (manualApiUrl.trim()) {
        localStorage.setItem('kairos.desk.apiUrl', manualApiUrl.trim());
      }
      if (manualToken.trim()) {
        localStorage.setItem('kairos.desk.token', manualToken.trim());
      }
    } catch {
      // ignore storage errors
    }
    client.updateCredentials({
      baseUrl: manualApiUrl.trim() || undefined,
      token: manualToken.trim() || undefined,
    });
    setLoading(true);
    await refreshBootstrap(client);
    client.disconnectWs();
    client.connectWs();
    setLoading(false);
  };

  if (loading && !bootstrap) {
    return (
      <div
        className="flex h-full w-full items-center justify-center"
        style={{ backgroundColor: 'var(--bg-canvas)', color: 'var(--text-secondary)' }}
      >
        <div className="flex items-center gap-2.5 text-[13px]">
          <RefreshCw className="h-4 w-4 animate-spin" style={{ color: 'var(--accent-brand)' }} />
          <span>Connecting to Kairos daemon...</span>
        </div>
      </div>
    );
  }

  if (!client || !bootstrap) {
    return (
      <div
        className="flex h-full w-full items-center justify-center p-6"
        style={{ backgroundColor: 'var(--bg-canvas)' }}
      >
        <div
          className="w-full max-w-[440px] rounded-[8px] border p-5"
          style={{
            backgroundColor: 'var(--bg-surface)',
            borderColor: 'var(--border-strong)',
          }}
        >
          <div className="mb-3 flex items-center gap-2">
            <AlertTriangle className="h-4 w-4" style={{ color: 'var(--state-warn)' }} />
            <h1 className="font-editorial text-[18px] font-medium">
              Kairos Daemon Unavailable
            </h1>
          </div>
          <p className="mb-4 text-[12px] leading-relaxed" style={{ color: 'var(--text-secondary)' }}>
            {connError || 'Unable to fetch initial state from kairosd.'}
          </p>
          <form onSubmit={(e) => void handleManualConnect(e)} className="space-y-2.5">
            <input
              type="text"
              value={manualApiUrl}
              onChange={(e) => setManualApiUrl(e.target.value)}
              placeholder="Daemon URL (optional in Vite proxy, e.g. http://127.0.0.1:9530)"
              className="tabular-nums w-full rounded-[6px] border px-2.5 py-1.5 text-[12px] outline-none"
              style={{
                backgroundColor: 'var(--bg-canvas)',
                borderColor: 'var(--border-hairline)',
                color: 'var(--text-primary)',
              }}
            />
            <input
              type="password"
              value={manualToken}
              onChange={(e) => setManualToken(e.target.value)}
              placeholder="Bearer token from KAIROSD_DESK_READY"
              className="tabular-nums w-full rounded-[6px] border px-2.5 py-1.5 text-[12px] outline-none"
              style={{
                backgroundColor: 'var(--bg-canvas)',
                borderColor: 'var(--border-hairline)',
                color: 'var(--text-primary)',
              }}
            />
            <div className="flex justify-end gap-2 pt-1">
              <button
                type="submit"
                className="rounded-[6px] px-3 py-1.5 text-[12px] font-medium"
                style={{ backgroundColor: 'var(--accent-brand)', color: '#FAF9F5' }}
              >
                Connect & Retry
              </button>
            </div>
          </form>
        </div>
      </div>
    );
  }

  return (
    <div
      className="flex h-full w-full flex-col overflow-hidden"
      style={{ backgroundColor: 'var(--bg-canvas)', color: 'var(--text-primary)' }}
    >
      <TopStatusBar
        activeView={activeView}
        onChangeView={setActiveView}
        status={bootstrap.status}
        activeSymbol={activeSymbol}
        wsConnected={wsConnected}
        theme={theme}
        onToggleTheme={handleToggleTheme}
        onOpenCommandPalette={handleOpenPalette}
        onOpenSettings={handleOpenSettings}
        onSelectSymbol={selectTradeableSymbol}
      />

      {connError && (
        <div
          className="flex shrink-0 items-center justify-between border-b px-3 py-1 text-[11px]"
          style={{
            backgroundColor: 'var(--state-warn-soft)',
            borderColor: 'var(--border-hairline)',
            color: 'var(--state-warn)',
          }}
        >
          <span>Daemon warning: {connError}</span>
          <button
            type="button"
            onClick={() => void refreshBootstrap()}
            className="underline"
          >
            Retry
          </button>
        </div>
      )}

      <main className="min-h-0 flex-1 overflow-hidden">
        {activeView === 'watch' && (
          <WatchWorkspace
            client={client}
            bootstrap={bootstrap}
            activeSymbol={activeSymbol}
            onSelectSymbol={selectTradeableSymbol}
            splitMode={splitMode}
            onToggleSplit={handleToggleSplit}
            activeTf={activeTf}
            onSelectTf={setActiveTf}
          />
        )}

        {activeView === 'review' && (
          <ReviewWorkspace
            client={client}
            bootstrap={bootstrap}
            initialSymbol={reviewTarget.symbol || activeSymbol}
            initialCursorTs={reviewTarget.cursorTs}
            initialTicketId={reviewTarget.ticketId}
            onSelectSymbol={selectTradeableSymbol}
            onRefreshBootstrap={() => refreshBootstrap()}
          />
        )}

        {activeView === 'stats' && (
          <StatsWorkspace
            client={client}
            bootstrap={bootstrap}
            onDrillDownReview={handleOpenInReview}
          />
        )}
      </main>

      <CommandPalette
        open={paletteOpen}
        onClose={handleClosePalette}
        bootstrap={bootstrap}
        activeSymbol={activeSymbol}
        onSelectSymbol={selectTradeableSymbol}
        onChangeView={setActiveView}
        onOpenTicketInReview={handleOpenInReview}
        onToggleSplit={handleToggleSplit}
        onToggleTheme={handleToggleTheme}
        onSyncAccount={handleTriggerSync}
        onOpenSettings={handleOpenSettings}
      />

      <SettingsModal
        open={settingsOpen}
        onClose={handleCloseSettings}
        status={bootstrap.status}
        chimeEnabled={chimeEnabled}
        onToggleChime={handleToggleChime}
        syncingAccount={syncingAccount}
        onSyncAccount={handleSyncAccount}
        onSaveSettings={handleSaveSettings}
      />
    </div>
  );
}
