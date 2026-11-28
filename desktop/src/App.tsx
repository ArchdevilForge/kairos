import React, { Suspense, lazy, useCallback, useEffect, useRef, useState } from 'react';
import { AlertTriangle, RefreshCw } from 'lucide-react';
import { AppShellSkeleton } from './components/Skeleton';
import { ToastProvider, useToast } from './components/ToastViewport';
import { TopStatusBar, type WorkspaceTab } from './components/TopStatusBar';
import { t as i18nT, useI18n } from './i18n';
import { DeskClient } from './services/deskClient';
import type {
  AccountSyncResponse,
  AnomalyItem,
  BootstrapPayload,
  MarketState,
  ReviewSubjectType,
  Timeframe,
  UpdateSettingsRequest,
  WsEvent,
} from './types/kairos';
import { formatExchangeLabel, resolveAccountExchange } from './utils/doctrine';
import { createCoalescedRunner, type CoalescedRunner } from './utils/asyncGuards';
import { WatchWorkspace } from './views/WatchWorkspace';

const ReviewWorkspace = lazy(() =>
  import('./views/ReviewWorkspace').then((m) => ({ default: m.ReviewWorkspace })),
);
const StatsWorkspace = lazy(() =>
  import('./views/StatsWorkspace').then((m) => ({ default: m.StatsWorkspace })),
);
const CommandPalette = lazy(() =>
  import('./components/CommandPalette').then((m) => ({ default: m.CommandPalette })),
);
const SettingsModal = lazy(() =>
  import('./components/SettingsModal').then((m) => ({ default: m.SettingsModal })),
);
const ShortcutsModal = lazy(() =>
  import('./components/ShortcutsModal').then((m) => ({ default: m.ShortcutsModal })),
);

const THEME_STORAGE_KEY = 'kairos.desk.theme';
const CHIME_STORAGE_KEY = 'kairos.desk.chime';
const SYMBOL_STORAGE_KEY = 'kairos.desk.symbol';

function isEditableElement(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  const tag = target.tagName;
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT';
}

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
  return (
    <ToastProvider>
      <AppShell />
    </ToastProvider>
  );
}

function AppShell() {
  const { t, locale } = useI18n();
  const { pushToast } = useToast();
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
    subjectKind?: ReviewSubjectType;
    subjectId?: string;
  }>({ symbol: 'BTC/USDT:USDT' });

  const [theme, setTheme] = useState<'dark' | 'light'>(readInitialTheme);
  const [chimeEnabled, setChimeEnabled] = useState<boolean>(readInitialChime);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
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
  const hadWsConnectedRef = useRef(false);
  const pushToastRef = useRef(pushToast);
  pushToastRef.current = pushToast;
  const tRef = useRef(t);
  tRef.current = t;
  const mountedRef = useRef(true);
  const bootstrapRunnerRef = useRef<CoalescedRunner<DeskClient> | null>(null);
  const lastSyncAtRef = useRef<number>(0);
  const lastAccountWsMsRef = useRef<number>(0);
  const syncInFlightRef = useRef<boolean>(false);
  const initialAutoSyncDoneRef = useRef<boolean>(false);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      bootstrapRunnerRef.current?.reset();
    };
  }, []);

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

  if (!bootstrapRunnerRef.current) {
    bootstrapRunnerRef.current = createCoalescedRunner<DeskClient>(async (active) => {
      try {
        const data = await active.bootstrap();
        if (!mountedRef.current) return;
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
        if (data.status?.account?.last_sync_at) {
          lastSyncAtRef.current = data.status.account.last_sync_at;
        }
        setBootstrap({ ...data, notices: visibleNotices });
        setConnError(null);
        setActiveSymbol((prev) => {
          const clean = sanitizeSymbol(prev, '');
          if (clean) return clean;
          return sanitizeSymbol(data.status.symbols[0], 'BTC/USDT:USDT');
        });
      } catch (err) {
        if (!mountedRef.current) return;
        setConnError(
          err instanceof Error ? err.message : tRef.current('app.err.unreachable'),
        );
      }
    });
  }

  const refreshBootstrap = useCallback(
    async (targetClient?: DeskClient) => {
      const active = targetClient ?? client;
      if (!active || !bootstrapRunnerRef.current) return;
      await bootstrapRunnerRef.current.run(active);
    },
    [client],
  );
  const refreshBootstrapRef = useRef(refreshBootstrap);
  refreshBootstrapRef.current = refreshBootstrap;

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
          if (cancelled) return;
          setWsConnected(connected);
          if (connected) {
            if (hadWsConnectedRef.current) {
              pushToastRef.current({
                id: 'ws-state',
                tone: 'success',
                title: i18nT('toast.wsReconnected'),
                durationMs: 2400,
              });
            }
            hadWsConnectedRef.current = true;
          } else if (hadWsConnectedRef.current) {
            pushToastRef.current({
              id: 'ws-state',
              tone: 'warn',
              title: i18nT('toast.wsDisconnected'),
              durationMs: 3600,
            });
          }
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
            const nextSyncAt = evt.account?.last_sync_at ?? 0;
            const nextWsMs = evt.synced_at_ms ?? 0;
            const hasFreshSync =
              !evt.account?.sync_error &&
              ((nextWsMs > 0 && nextWsMs > lastAccountWsMsRef.current) ||
                (nextSyncAt > 0 && nextSyncAt !== lastSyncAtRef.current));
            if (hasFreshSync) {
              if (nextWsMs > 0) lastAccountWsMsRef.current = nextWsMs;
              if (nextSyncAt > 0) lastSyncAtRef.current = nextSyncAt;
              void refreshBootstrapRef.current(c);
            }
          } else if (evt.type === 'catalog_updated') {
            setBootstrap((prev) =>
              prev
                ? {
                    ...prev,
                    status: {
                      ...prev.status,
                      available_symbols:
                        evt.available_symbols?.length > 0
                          ? evt.available_symbols
                          : prev.status.available_symbols,
                      symbol_catalog:
                        evt.symbol_catalog && evt.symbol_catalog.length > 0
                          ? evt.symbol_catalog
                          : prev.status.symbol_catalog,
                    },
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
            err instanceof Error ? err.message : i18nT('app.err.initFailed'),
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
      if (!client || syncInFlightRef.current) return undefined;
      syncInFlightRef.current = true;
      setSyncingAccount(true);
      try {
        const res = await client.syncAccount();
        await refreshBootstrap(client);
        const exchangeLabel = formatExchangeLabel(
          resolveAccountExchange(bootstrap?.status),
          locale,
        );
        if (!opts?.rethrow) {
          pushToast({
            tone: 'success',
            title: t('toast.syncSuccessTitle', { exchange: exchangeLabel }),
            description: res?.account
              ? t('toast.syncSuccessDesc', {
                  balance: res.account.wallet_balance_usd.toFixed(2),
                  trades: res.synced_trades,
                  fills: res.synced_fills,
                })
              : undefined,
          });
        }
        return res;
      } catch (err) {
        if (opts?.rethrow) {
          throw err;
        }
        const message = err instanceof Error ? err.message : t('app.err.syncFailed');
        setConnError(message);
        pushToast({
          tone: 'error',
          title: t('toast.syncFailedTitle'),
          description: message,
        });
        return undefined;
      } finally {
        syncInFlightRef.current = false;
        setSyncingAccount(false);
      }
    },
    [bootstrap?.status, client, locale, pushToast, refreshBootstrap, t],
  );

  const triggerAutoSync = useCallback(
    async (ifStaleSec: number) => {
      if (!client || !bootstrap?.status?.account?.configured || syncInFlightRef.current) {
        return;
      }
      syncInFlightRef.current = true;
      setSyncingAccount(true);
      try {
        const res = await client.syncAccount({ ifStaleSec });
        if (res && !res.skipped) {
          await refreshBootstrap(client);
        }
      } catch {
        // Silent background auto-sync: backend persists account.sync_error without noisy toasts.
      } finally {
        syncInFlightRef.current = false;
        setSyncingAccount(false);
      }
    },
    [bootstrap?.status?.account?.configured, client, refreshBootstrap],
  );

  // Automatic initial account sync once credentials are detected on startup.
  useEffect(() => {
    if (!client || !bootstrap?.status?.account?.configured) return;
    if (initialAutoSyncDoneRef.current) return;
    initialAutoSyncDoneRef.current = true;
    void triggerAutoSync(30);
  }, [client, bootstrap?.status?.account?.configured, triggerAutoSync]);

  // Automatically sync fresh trades/positions whenever switching into Review or Stats.
  useEffect(() => {
    if (activeView !== 'review' && activeView !== 'stats') return;
    if (!bootstrap?.status?.account?.configured) return;
    void triggerAutoSync(20);
  }, [activeView, bootstrap?.status?.account?.configured, triggerAutoSync]);

  // Automatically sync when returning to the window while on Review or Stats.
  useEffect(() => {
    if (activeView !== 'review' && activeView !== 'stats') return;
    if (!bootstrap?.status?.account?.configured) return;
    const onVisible = () => {
      if (document.visibilityState === 'visible') {
        void triggerAutoSync(30);
      }
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => document.removeEventListener('visibilitychange', onVisible);
  }, [activeView, bootstrap?.status?.account?.configured, triggerAutoSync]);

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

  const modalStateRef = useRef({
    paletteOpen: false,
    settingsOpen: false,
    shortcutsOpen: false,
  });
  modalStateRef.current = { paletteOpen, settingsOpen, shortcutsOpen };

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      const m = modalStateRef.current;
      const anyModalOpen = m.paletteOpen || m.settingsOpen || m.shortcutsOpen;

      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        e.stopImmediatePropagation();
        setSettingsOpen(false);
        setShortcutsOpen(false);
        setPaletteOpen((prev) => !prev);
        return;
      }

      if (e.key === 'Escape' && anyModalOpen) {
        e.preventDefault();
        e.stopImmediatePropagation();
        if (m.paletteOpen) setPaletteOpen(false);
        else if (m.settingsOpen) setSettingsOpen(false);
        else if (m.shortcutsOpen) setShortcutsOpen(false);
        return;
      }

      if (e.key === '?' && !e.metaKey && !e.ctrlKey && !e.altKey) {
        if (isEditableElement(e.target) || m.paletteOpen || m.settingsOpen) {
          return;
        }
        e.preventDefault();
        e.stopImmediatePropagation();
        setShortcutsOpen((prev) => !prev);
        return;
      }

      // When any top-level modal is open, block background global workspace hotkeys
      if (anyModalOpen && !e.metaKey && !e.ctrlKey && !e.altKey) {
        const key = e.key;
        if (
          key === ' ' ||
          key === 'ArrowLeft' ||
          key === 'ArrowRight' ||
          key === 'Delete' ||
          key === 'Backspace' ||
          (key >= '1' && key <= '5') ||
          ['s', 'j', 'k', 'a', 'n'].includes(key.toLowerCase())
        ) {
          const target = e.target;
          if (!isEditableElement(target)) {
            // Allow Enter/Tab/Arrows inside modal buttons/lists; block workspace-only hotkeys
            if (key !== 'ArrowLeft' && key !== 'ArrowRight' && key !== ' ') {
              e.stopImmediatePropagation();
            } else if (!(target instanceof HTMLButtonElement)) {
              e.stopImmediatePropagation();
            }
          }
        }
      }
    };
    window.addEventListener('keydown', onKeyDown, { capture: true });
    return () => window.removeEventListener('keydown', onKeyDown, { capture: true });
  }, []);

  const handleOpenInReview = useCallback(
    (
      symbol: string,
      cursorTs?: number,
      ticketId?: string,
      subjectKind?: ReviewSubjectType,
      subjectId?: string,
    ) => {
      const clean = sanitizeSymbol(symbol);
      setActiveSymbol(clean);
      setReviewTarget({
        symbol: clean,
        cursorTs,
        ticketId,
        subjectKind,
        subjectId,
      });
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
      pushToast({
        tone: 'success',
        title: t('toast.settingsSaved'),
      });
      return syncRes;
    },
    [client, pushToast, refreshBootstrap, t],
  );

  const handleToggleTheme = useCallback(() => {
    setTheme((prev) => (prev === 'dark' ? 'light' : 'dark'));
  }, []);

  const handleToggleSplit = useCallback(() => {
    setSplitMode((prev) => !prev);
  }, []);

  const handleOpenPalette = useCallback(() => {
    setSettingsOpen(false);
    setShortcutsOpen(false);
    setPaletteOpen(true);
  }, []);

  const handleClosePalette = useCallback(() => {
    setPaletteOpen(false);
  }, []);

  const handleOpenSettings = useCallback(() => {
    setPaletteOpen(false);
    setShortcutsOpen(false);
    setSettingsOpen(true);
  }, []);

  const handleCloseSettings = useCallback(() => {
    setSettingsOpen(false);
  }, []);

  const handleOpenShortcuts = useCallback(() => {
    setPaletteOpen(false);
    setSettingsOpen(false);
    setShortcutsOpen(true);
  }, []);

  const handleCloseShortcuts = useCallback(() => {
    setShortcutsOpen(false);
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
    return <AppShellSkeleton label={t('app.loading')} />;
  }

  if (!client || !bootstrap) {
    return (
      <div
        className="flex h-full w-full items-center justify-center p-6"
        style={{ backgroundColor: 'var(--bg-canvas)' }}
      >
        <div className="kairos-floating kairos-animate-modal w-full max-w-[440px] rounded-[10px] p-5">
          <div className="mb-3 flex items-center gap-2">
            <AlertTriangle className="h-4 w-4" style={{ color: 'var(--state-warn)' }} />
            <h1 className="font-editorial text-[18px] font-medium">
              {t('app.daemonUnavailable')}
            </h1>
          </div>
          <p
            className="mb-4 text-[12px] leading-relaxed"
            style={{ color: 'var(--text-secondary)' }}
          >
            {connError || t('app.daemonUnavailableDesc')}
          </p>
          <form onSubmit={(e) => void handleManualConnect(e)} className="space-y-2.5">
            <input
              type="text"
              value={manualApiUrl}
              onChange={(e) => setManualApiUrl(e.target.value)}
              placeholder={t('app.daemonUrlPlaceholder')}
              className="tabular-nums w-full rounded-[6px] border px-2.5 py-1.5 text-[12px] outline-none transition-colors focus:border-[var(--accent-brand)]"
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
              placeholder={t('app.tokenPlaceholder')}
              className="tabular-nums w-full rounded-[6px] border px-2.5 py-1.5 text-[12px] outline-none transition-colors focus:border-[var(--accent-brand)]"
              style={{
                backgroundColor: 'var(--bg-canvas)',
                borderColor: 'var(--border-hairline)',
                color: 'var(--text-primary)',
              }}
            />
            <div className="flex justify-end gap-2 pt-1">
              <button
                type="submit"
                className="kairos-press rounded-[6px] px-3 py-1.5 text-[12px] font-medium hover:brightness-105"
                style={{ backgroundColor: 'var(--accent-brand-solid)', color: '#FAF9F5' }}
              >
                {t('app.connectRetry')}
              </button>
            </div>
          </form>
        </div>
      </div>
    );
  }

  const anyModalOpen = paletteOpen || settingsOpen || shortcutsOpen;

  return (
    <div
      className="flex h-full w-full flex-col overflow-hidden"
      style={{ backgroundColor: 'var(--bg-canvas)', color: 'var(--text-primary)' }}
    >
      <div
        inert={anyModalOpen ? true : undefined}
        className="flex min-h-0 flex-1 flex-col overflow-hidden"
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
          onOpenShortcuts={handleOpenShortcuts}
          onSelectSymbol={selectTradeableSymbol}
        />

        {connError ? (
          <div
            className="kairos-animate-view flex shrink-0 items-center justify-between border-b px-3 py-1.5 text-[11px]"
            style={{
              backgroundColor: 'var(--state-warn-soft)',
              borderColor: 'var(--border-hairline)',
              color: 'var(--state-warn)',
            }}
          >
            <span>{t('app.daemonWarning', { message: connError })}</span>
            <button
              type="button"
              onClick={() => void refreshBootstrap()}
              className="kairos-press font-medium underline"
            >
              {t('app.retry')}
            </button>
          </div>
        ) : null}

        <main className="min-h-0 flex-1 overflow-hidden">
          {activeView === 'watch' ? (
            <div key="view-watch" className="kairos-animate-view h-full w-full">
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
            </div>
          ) : null}

          {activeView === 'review' ? (
            <div key="view-review" className="kairos-animate-view h-full w-full">
              <Suspense
                fallback={
                  <div
                    className="flex h-full w-full items-center justify-center text-xs"
                    style={{ color: 'var(--text-secondary)' }}
                  >
                    <RefreshCw
                      className="mr-2 h-4 w-4 animate-spin"
                      style={{ color: 'var(--accent-brand)' }}
                    />
                  </div>
                }
              >
                <ReviewWorkspace
                  client={client}
                  bootstrap={bootstrap}
                  initialSymbol={reviewTarget.symbol || activeSymbol}
                  initialCursorTs={reviewTarget.cursorTs}
                  initialTicketId={reviewTarget.ticketId}
                  initialSubjectKind={reviewTarget.subjectKind}
                  initialSubjectId={reviewTarget.subjectId}
                  onSelectSymbol={selectTradeableSymbol}
                  onRefreshBootstrap={() => refreshBootstrap()}
                  syncingAccount={syncingAccount}
                  onSyncAccount={handleSyncAccount}
                />
              </Suspense>
            </div>
          ) : null}

          {activeView === 'stats' ? (
            <div key="view-stats" className="kairos-animate-view h-full w-full">
              <Suspense
                fallback={
                  <div
                    className="flex h-full w-full items-center justify-center text-xs"
                    style={{ color: 'var(--text-secondary)' }}
                  >
                    <RefreshCw
                      className="mr-2 h-4 w-4 animate-spin"
                      style={{ color: 'var(--accent-brand)' }}
                    />
                  </div>
                }
              >
                <StatsWorkspace
                  client={client}
                  bootstrap={bootstrap}
                  onDrillDownReview={handleOpenInReview}
                  syncingAccount={syncingAccount}
                  onSyncAccount={handleSyncAccount}
                />
              </Suspense>
            </div>
          ) : null}
        </main>
      </div>

      {paletteOpen ? (
        <Suspense fallback={null}>
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
            onOpenShortcuts={handleOpenShortcuts}
          />
        </Suspense>
      ) : null}

      {settingsOpen ? (
        <Suspense fallback={null}>
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
        </Suspense>
      ) : null}

      {shortcutsOpen ? (
        <Suspense fallback={null}>
          <ShortcutsModal open={shortcutsOpen} onClose={handleCloseShortcuts} />
        </Suspense>
      ) : null}
    </div>
  );
}
