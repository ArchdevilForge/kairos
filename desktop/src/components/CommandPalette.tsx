import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  ArrowRight,
  BarChart3,
  Columns2,
  Eye,
  History,
  Keyboard,
  Languages,
  RefreshCw,
  Search,
  Settings,
  SunMoon,
} from 'lucide-react';
import { useI18n } from '../i18n';
import type { BootstrapPayload, SymbolCatalogItem } from '../types/kairos';
import { formatExchangeLabel, resolveAccountExchange } from '../utils/doctrine';
import type { WorkspaceTab } from './TopStatusBar';

export interface CommandPaletteProps {
  open: boolean;
  onClose: () => void;
  bootstrap: BootstrapPayload;
  activeSymbol: string;
  onSelectSymbol: (symbol: string) => void;
  onChangeView: (view: WorkspaceTab) => void;
  onOpenTicketInReview: (symbol: string, cursorTs?: number, ticketId?: string) => void;
  onToggleSplit: () => void;
  onToggleTheme: () => void;
  onSyncAccount: () => void;
  onOpenSettings: () => void;
  onOpenShortcuts?: () => void;
}

interface PaletteItem {
  id: string;
  group: string;
  label: string;
  hint?: string;
  searchText?: string;
  tickerKey?: string;
  underlyingKey?: string;
  fullNameKey?: string;
  shortcut?: string;
  icon: React.ReactNode;
  run: () => void;
}

function normalizeManualSymbol(raw: string): string | null {
  const cleaned = raw.trim().toUpperCase();
  if (!cleaned || cleaned === 'MARKET' || cleaned.length < 1 || /\s/.test(cleaned)) {
    return null;
  }
  if (cleaned.includes('/') && cleaned.includes(':')) return cleaned;
  if (cleaned.includes('/')) {
    const [base, quote] = cleaned.split('/');
    if (!base || !quote) return null;
    return `${base}/${quote}:USDT`;
  }
  const base = cleaned.replace(/USDT$/, '');
  if (!base || base === 'MARKET' || !/^[A-Z0-9]{1,16}$/.test(base)) return null;
  return `${base}/USDT:USDT`;
}

function assetTypeAliases(symbolType: string | undefined): string {
  switch ((symbolType ?? '').toLowerCase()) {
    case 'stock':
      return 'stock stocks equity us 美股 股票 科技股';
    case 'etf':
      return 'etf fund index 美股 指数 基金';
    case 'commodity':
      return 'commodity commodities metal gold oil 大宗 商品 黄金';
    case 'forex':
      return 'forex fx currency 外汇 汇率';
    case 'index':
      return 'index 指数';
    case 'tradfi':
      return 'tradfi 美股 传统金融';
    default:
      return '';
  }
}

function scorePaletteItem(item: PaletteItem, q: string): number {
  const ticker = item.tickerKey ?? '';
  const underlying = item.underlyingKey ?? '';
  const fullName = item.fullNameKey ?? '';
  if (ticker === q || underlying === q) return 0;
  if (fullName === q) return 1;
  if (ticker.startsWith(q) || underlying.startsWith(q)) return 2;
  if (fullName.startsWith(q)) return 3;
  if (fullName.includes(` ${q}`)) return 4;
  if (item.label.toLowerCase().includes(q)) return 5;
  if (fullName.includes(q)) return 6;
  return 7;
}

export const CommandPalette: React.FC<CommandPaletteProps> = ({
  open,
  onClose,
  bootstrap,
  activeSymbol,
  onSelectSymbol,
  onChangeView,
  onOpenTicketInReview,
  onToggleSplit,
  onToggleTheme,
  onSyncAccount,
  onOpenSettings,
  onOpenShortcuts,
}) => {
  const { locale, toggleLocale, t } = useI18n();
  const activeExchange = resolveAccountExchange(bootstrap.status);
  const exchangeLabel = formatExchangeLabel(activeExchange, locale);
  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const rowRefs = useRef<Map<number, HTMLButtonElement>>(new Map());
  const prevFocusRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!open) return;
    prevFocusRef.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setQuery('');
    setSelectedIndex(0);
    const timer = window.setTimeout(() => inputRef.current?.focus(), 10);
    return () => {
      window.clearTimeout(timer);
      const prev = prevFocusRef.current;
      prevFocusRef.current = null;
      if (prev && prev.isConnected) {
        requestAnimationFrame(() => prev.focus());
      }
    };
  }, [open]);

  const items = useMemo<PaletteItem[]>(() => {
    const q = query.trim().toLowerCase();
    const viewsGroup = t('palette.group.views');
    const actionsGroup = t('palette.group.actions');
    const list: PaletteItem[] = [
      {
        id: 'action:watch',
        group: viewsGroup,
        label: t('palette.action.watchLabel'),
        hint: t('palette.action.watchHint'),
        icon: <Eye className="h-3.5 w-3.5" />,
        run: () => {
          onChangeView('watch');
          onClose();
        },
      },
      {
        id: 'action:review',
        group: viewsGroup,
        label: t('palette.action.reviewLabel'),
        hint: t('palette.action.reviewHint'),
        icon: <History className="h-3.5 w-3.5" />,
        run: () => {
          onChangeView('review');
          onClose();
        },
      },
      {
        id: 'action:stats',
        group: viewsGroup,
        label: t('palette.action.statsLabel'),
        hint: t('palette.action.statsHint'),
        icon: <BarChart3 className="h-3.5 w-3.5" />,
        run: () => {
          onChangeView('stats');
          onClose();
        },
      },
      {
        id: 'action:split',
        group: actionsGroup,
        label: t('palette.action.splitLabel'),
        hint: t('palette.action.splitHint'),
        shortcut: 'S',
        icon: <Columns2 className="h-3.5 w-3.5" />,
        run: () => {
          onToggleSplit();
          onClose();
        },
      },
      {
        id: 'action:theme',
        group: actionsGroup,
        label: t('palette.action.themeLabel'),
        icon: <SunMoon className="h-3.5 w-3.5" />,
        run: () => {
          onToggleTheme();
          onClose();
        },
      },
      {
        id: 'action:language',
        group: actionsGroup,
        label: t('palette.action.langLabel'),
        hint:
          locale === 'zh'
            ? t('palette.action.langHintToEn')
            : t('palette.action.langHintToZh'),
        icon: <Languages className="h-3.5 w-3.5" />,
        run: () => {
          toggleLocale();
          onClose();
        },
      },
      {
        id: 'action:sync',
        group: actionsGroup,
        label: t('palette.action.syncLabel', { exchange: exchangeLabel }),
        icon: <RefreshCw className="h-3.5 w-3.5" />,
        run: () => {
          onSyncAccount();
          onClose();
        },
      },
      {
        id: 'action:settings',
        group: actionsGroup,
        label: t('palette.action.settingsLabel'),
        icon: <Settings className="h-3.5 w-3.5" />,
        run: () => {
          onOpenSettings();
          onClose();
        },
      },
    ];

    if (onOpenShortcuts) {
      list.push({
        id: 'action:shortcuts',
        group: actionsGroup,
        label: t('palette.action.shortcutsLabel'),
        hint: t('palette.action.shortcutsHint'),
        shortcut: '?',
        icon: <Keyboard className="h-3.5 w-3.5" />,
        run: () => {
          onClose();
          onOpenShortcuts();
        },
      });
    }

    const catalogList = bootstrap.status.symbol_catalog ?? [];
    const hasFullCatalog = catalogList.length > 0;
    const catalogBySymbol = new Map<string, SymbolCatalogItem>();
    for (const entry of catalogList) {
      if (entry?.symbol) {
        catalogBySymbol.set(entry.symbol.toUpperCase(), entry);
      }
    }

    const assetLabelFor = (symbolType: string | undefined): string => {
      switch ((symbolType ?? '').toLowerCase()) {
        case 'stock':
          return t('palette.asset.stock');
        case 'etf':
          return t('palette.asset.etf');
        case 'commodity':
          return t('palette.asset.commodity');
        case 'forex':
          return t('palette.asset.forex');
        case 'index':
          return t('palette.asset.index');
        case 'tradfi':
          return t('palette.asset.tradfi');
        default:
          return '';
      }
    };

    const rawSymbols =
      bootstrap.status.available_symbols && bootstrap.status.available_symbols.length > 0
        ? bootstrap.status.available_symbols
        : bootstrap.status.symbols || [];
    const symbols = Array.from(
      new Set(
        [activeSymbol, ...rawSymbols].filter(
          (s): s is string => Boolean(s) && s.toUpperCase() !== 'MARKET',
        ),
      ),
    );
    const allowedSet = new Set(rawSymbols.map((s) => s.toUpperCase()));
    const symbolsGroup = t('palette.group.symbols');
    for (const sym of symbols) {
      const meta = catalogBySymbol.get(sym.toUpperCase());
      const assetLabel = assetLabelFor(meta?.symbol_type);
      const baseTicker = sym.replace(/\/USDT(:USDT)?$/i, '').replace(/:USDT$/i, '');
      const hintParts: string[] = [];
      if (meta?.full_name) hintParts.push(meta.full_name);
      if (assetLabel) hintParts.push(assetLabel);
      if (sym === activeSymbol) {
        hintParts.push(t('palette.symbol.current'));
      } else if (hintParts.length === 0) {
        hintParts.push(t('palette.symbol.switchWatch'));
      }
      const searchParts = [
        sym,
        baseTicker,
        `${baseTicker}usdt`,
        meta?.full_name ?? '',
        meta?.underlying_ticker ?? '',
        meta?.symbol_type ?? '',
        assetLabel,
        assetTypeAliases(meta?.symbol_type),
      ];
      list.push({
        id: `sym:${sym}`,
        group: symbolsGroup,
        label: sym.replace(/:USDT$/, ''),
        hint: hintParts.join(' · '),
        searchText: searchParts.filter(Boolean).join(' ').toLowerCase(),
        tickerKey: baseTicker.toLowerCase(),
        underlyingKey: (meta?.underlying_ticker ?? '').toLowerCase(),
        fullNameKey: (meta?.full_name ?? '').toLowerCase(),
        icon: <ArrowRight className="h-3.5 w-3.5" />,
        run: () => {
          onSelectSymbol(sym);
          onChangeView('watch');
          onClose();
        },
      });
    }

    const ticketsGroup = t('palette.group.tickets');
    for (const bundle of bootstrap.tickets.slice(0, 20)) {
      const ticket = bundle.ticket;
      const dirLabel =
        ticket.direction === 'down' ? t('palette.dir.short') : t('palette.dir.long');
      list.push({
        id: `ticket:${ticket.id}`,
        group: ticketsGroup,
        label: `${ticket.symbol.replace(/:USDT$/, '')} · ${dirLabel} · ${ticket.grade}`,
        hint: `${ticket.status} · ${ticket.id}`,
        icon: <History className="h-3.5 w-3.5" />,
        run: () => {
          onOpenTicketInReview(ticket.symbol, ticket.signal_at || ticket.created_at, ticket.id);
          onClose();
        },
      });
    }

    const filtered = q
      ? list
          .filter(
            (item) =>
              item.label.toLowerCase().includes(q) ||
              (item.hint && item.hint.toLowerCase().includes(q)) ||
              (item.searchText && item.searchText.includes(q)) ||
              item.group.toLowerCase().includes(q),
          )
          .sort((a, b) => scorePaletteItem(a, q) - scorePaletteItem(b, q))
      : list;

    const hasMatchedSymbol = filtered.some((item) => item.id.startsWith('sym:'));
    const customSym = normalizeManualSymbol(query);
    if (
      customSym &&
      !symbols.includes(customSym) &&
      !hasMatchedSymbol &&
      (!hasFullCatalog || allowedSet.has(customSym.toUpperCase()))
    ) {
      filtered.unshift({
        id: `custom:${customSym}`,
        group: t('palette.group.jump'),
        label: customSym.replace(/:USDT$/, ''),
        hint: t('palette.symbol.openWatch'),
        icon: <ArrowRight className="h-3.5 w-3.5" />,
        run: () => {
          onSelectSymbol(customSym);
          onChangeView('watch');
          onClose();
        },
      });
    }

    return filtered.slice(0, 28);
  }, [
    activeSymbol,
    bootstrap.status.available_symbols,
    bootstrap.status.symbol_catalog,
    bootstrap.status.symbols,
    bootstrap.tickets,
    exchangeLabel,
    locale,
    onChangeView,
    onClose,
    onOpenSettings,
    onOpenShortcuts,
    onOpenTicketInReview,
    onSelectSymbol,
    onSyncAccount,
    onToggleSplit,
    onToggleTheme,
    query,
    t,
    toggleLocale,
  ]);

  useEffect(() => {
    setSelectedIndex((prev) =>
      items.length === 0 ? 0 : Math.min(prev, items.length - 1),
    );
  }, [items.length]);

  // Keep active keyboard-selected row smoothly scrolled into view
  useEffect(() => {
    if (!open) return;
    const activeRow = rowRefs.current.get(selectedIndex);
    activeRow?.scrollIntoView({ block: 'nearest' });
  }, [open, selectedIndex]);

  if (!open) return null;

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      onClose();
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (items.length > 0) {
        setSelectedIndex((prev) => (prev + 1) % items.length);
      }
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (items.length > 0) {
        setSelectedIndex((prev) => (prev - 1 + items.length) % items.length);
      }
    } else if (e.key === 'Enter') {
      // If user tabbed onto a specific result button, let its native click run that button
      if (e.target instanceof HTMLButtonElement) {
        return;
      }
      e.preventDefault();
      const target = items[selectedIndex];
      if (target) target.run();
    }
  };

  return (
    <div
      className="kairos-glass-backdrop kairos-animate-backdrop fixed inset-0 z-50 flex items-start justify-center px-3 pt-[9dvh] sm:pt-[11dvh]"
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t('palette.ariaLabel')}
        className="kairos-floating kairos-animate-palette kairos-contain-panel w-full max-w-[548px] overflow-hidden rounded-[10px]"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={handleKeyDown}
      >
        <div
          className="flex items-center gap-2.5 border-b px-3.5 py-2.5"
          style={{ borderColor: 'var(--border-hairline)' }}
        >
          <Search
            className="h-4 w-4 shrink-0"
            style={{ color: 'var(--accent-brand)' }}
          />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setSelectedIndex(0);
            }}
            placeholder={t('palette.searchPlaceholder')}
            className="w-full bg-transparent text-[13px] outline-none"
            style={{ color: 'var(--text-primary)' }}
          />
          <kbd className="kairos-kbd shrink-0">ESC</kbd>
        </div>

        <div className="max-h-[380px] overflow-y-auto p-1.5">
          {items.length === 0 ? (
            <div
              className="px-3 py-8 text-center text-[12px]"
              style={{ color: 'var(--text-muted)' }}
            >
              {t('palette.empty')}
            </div>
          ) : (
            items.map((item, idx) => {
              const active = idx === selectedIndex;
              const prevGroup = idx > 0 ? items[idx - 1].group : null;
              const showGroupHeader = item.group !== prevGroup;
              return (
                <React.Fragment key={item.id}>
                  {showGroupHeader ? (
                    <div
                      className={`px-2.5 pb-1 text-[10px] font-semibold uppercase tracking-wider ${
                        idx > 0 ? 'mt-2 pt-1.5 border-t' : 'pt-1'
                      }`}
                      style={{
                        color: 'var(--text-muted)',
                        borderColor: 'var(--border-hairline)',
                      }}
                    >
                      {item.group}
                    </div>
                  ) : null}
                  <button
                    ref={(el) => {
                      if (el) rowRefs.current.set(idx, el);
                      else rowRefs.current.delete(idx);
                    }}
                    type="button"
                    onMouseEnter={() => setSelectedIndex(idx)}
                    onFocus={() => setSelectedIndex(idx)}
                    onClick={item.run}
                    className="flex w-full items-center justify-between gap-2 rounded-[6px] px-2.5 py-2 text-left transition-colors duration-75"
                    style={{
                      backgroundColor: active ? 'var(--bg-elevated)' : 'transparent',
                      color: 'var(--text-primary)',
                      boxShadow: active ? 'var(--inset-highlight)' : undefined,
                    }}
                  >
                    <div className="flex min-w-0 items-center gap-2.5">
                      <span
                        className="flex h-6 w-6 shrink-0 items-center justify-center rounded-[5px] border transition-colors"
                        style={{
                          backgroundColor: active
                            ? 'var(--accent-brand-soft)'
                            : 'var(--bg-canvas)',
                          borderColor: active
                            ? 'var(--accent-brand-glow)'
                            : 'var(--border-hairline)',
                          color: active ? 'var(--accent-brand)' : 'var(--text-muted)',
                        }}
                      >
                        {item.icon}
                      </span>
                      <div className="min-w-0">
                        <div className="tabular-nums truncate text-[13px] font-medium">
                          {item.label}
                        </div>
                        {item.hint ? (
                          <div
                            className="tabular-nums truncate text-[11px]"
                            style={{ color: 'var(--text-secondary)' }}
                          >
                            {item.hint}
                          </div>
                        ) : null}
                      </div>
                    </div>
                    <div className="flex shrink-0 items-center gap-1.5">
                      {item.shortcut ? (
                        <kbd className="kairos-kbd">{item.shortcut}</kbd>
                      ) : active ? (
                        <kbd className="kairos-kbd">↵</kbd>
                      ) : null}
                    </div>
                  </button>
                </React.Fragment>
              );
            })
          )}
        </div>

        <div
          className="flex items-center justify-between border-t px-3.5 py-2 text-[10px]"
          style={{
            backgroundColor: 'var(--bg-canvas)',
            borderColor: 'var(--border-hairline)',
            color: 'var(--text-muted)',
          }}
        >
          <div className="flex items-center gap-3">
            <span className="flex items-center gap-1">
              <kbd className="kairos-kbd">↑</kbd>
              <kbd className="kairos-kbd">↓</kbd>
              <span>{t('palette.footer.navigate')}</span>
            </span>
            <span className="flex items-center gap-1">
              <kbd className="kairos-kbd">↵</kbd>
              <span>{t('palette.footer.confirm')}</span>
            </span>
          </div>
          <span className="flex items-center gap-1">
            <kbd className="kairos-kbd">ESC</kbd>
            <span>{t('palette.footer.close')}</span>
          </span>
        </div>
      </div>
    </div>
  );
};
