import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  ArrowRight,
  BarChart3,
  Columns2,
  Eye,
  History,
  RefreshCw,
  Search,
  Settings,
  SunMoon,
} from 'lucide-react';
import type { BootstrapPayload } from '../types/kairos';
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
}

interface PaletteItem {
  id: string;
  group: string;
  label: string;
  hint?: string;
  icon: React.ReactNode;
  run: () => void;
}

function normalizeManualSymbol(raw: string): string | null {
  const cleaned = raw.trim().toUpperCase();
  if (!cleaned || cleaned === 'MARKET' || cleaned.length < 2 || /\s/.test(cleaned)) {
    return null;
  }
  if (cleaned.includes('/') && cleaned.includes(':')) return cleaned;
  if (cleaned.includes('/')) {
    const [base, quote] = cleaned.split('/');
    if (!base || !quote) return null;
    return `${base}/${quote}:USDT`;
  }
  const base = cleaned.replace(/USDT$/, '');
  if (!base || base === 'MARKET' || !/^[A-Z0-9]{2,12}$/.test(base)) return null;
  return `${base}/USDT:USDT`;
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
}) => {
  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    if (!open) return;
    setQuery('');
    setSelectedIndex(0);
    const timer = setTimeout(() => inputRef.current?.focus(), 10);
    return () => clearTimeout(timer);
  }, [open]);

  const items = useMemo<PaletteItem[]>(() => {
    const q = query.trim().toLowerCase();
    const list: PaletteItem[] = [
      {
        id: 'action:watch',
        group: '视图',
        label: '看盘视图',
        hint: '实时 K 线与双图看盘',
        icon: <Eye className="h-3.5 w-3.5" />,
        run: () => {
          onChangeView('watch');
          onClose();
        },
      },
      {
        id: 'action:review',
        group: '视图',
        label: '复盘视图',
        hint: '零前视回放与历史复盘',
        icon: <History className="h-3.5 w-3.5" />,
        run: () => {
          onChangeView('review');
          onClose();
        },
      },
      {
        id: 'action:stats',
        group: '视图',
        label: '统计视图',
        hint: '资金净值、R 分布与风控合规',
        icon: <BarChart3 className="h-3.5 w-3.5" />,
        run: () => {
          onChangeView('stats');
          onClose();
        },
      },
      {
        id: 'action:split',
        group: '操作',
        label: '切换单图 / 双图 (s)',
        hint: '结构与触发同步联排',
        icon: <Columns2 className="h-3.5 w-3.5" />,
        run: () => {
          onToggleSplit();
          onClose();
        },
      },
      {
        id: 'action:theme',
        group: '操作',
        label: '切换暗色 / 亮色主题',
        icon: <SunMoon className="h-3.5 w-3.5" />,
        run: () => {
          onToggleTheme();
          onClose();
        },
      },
      {
        id: 'action:sync',
        group: '操作',
        label: '立即同步只读账户',
        icon: <RefreshCw className="h-3.5 w-3.5" />,
        run: () => {
          onSyncAccount();
          onClose();
        },
      },
      {
        id: 'action:settings',
        group: '操作',
        label: '打开风险与账户设置',
        icon: <Settings className="h-3.5 w-3.5" />,
        run: () => {
          onOpenSettings();
          onClose();
        },
      },
    ];

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
    for (const sym of symbols) {
      list.push({
        id: `sym:${sym}`,
        group: 'Crypto 永续',
        label: sym.replace(/:USDT$/, ''),
        hint: sym === activeSymbol ? '当前' : '切换看盘',
        icon: <ArrowRight className="h-3.5 w-3.5" />,
        run: () => {
          onSelectSymbol(sym);
          onChangeView('watch');
          onClose();
        },
      });
    }

    for (const bundle of bootstrap.tickets.slice(0, 20)) {
      const t = bundle.ticket;
      list.push({
        id: `ticket:${t.id}`,
        group: '候选票',
        label: `${t.symbol.replace(/:USDT$/, '')} · ${t.direction === 'down' ? '做空' : '做多'} · ${t.grade}`,
        hint: `${t.status} · ${t.id}`,
        icon: <History className="h-3.5 w-3.5" />,
        run: () => {
          onOpenTicketInReview(t.symbol, t.signal_at || t.created_at, t.id);
          onClose();
        },
      });
    }

    const filtered = q
      ? list.filter(
          (item) =>
            item.label.toLowerCase().includes(q) ||
            (item.hint && item.hint.toLowerCase().includes(q)) ||
            item.group.toLowerCase().includes(q),
        )
      : list;

    const customSym = normalizeManualSymbol(query);
    if (
      customSym &&
      !symbols.includes(customSym) &&
      (allowedSet.size === 0 || allowedSet.has(customSym.toUpperCase()))
    ) {
      filtered.unshift({
        id: `custom:${customSym}`,
        group: '跳转标的',
        label: customSym.replace(/:USDT$/, ''),
        hint: '看盘打开',
        icon: <ArrowRight className="h-3.5 w-3.5" />,
        run: () => {
          onSelectSymbol(customSym);
          onChangeView('watch');
          onClose();
        },
      });
    }

    return filtered.slice(0, 24);
  }, [
    activeSymbol,
    bootstrap.status.available_symbols,
    bootstrap.status.symbols,
    bootstrap.tickets,
    onChangeView,
    onClose,
    onOpenSettings,
    onOpenTicketInReview,
    onSelectSymbol,
    onSyncAccount,
    onToggleSplit,
    onToggleTheme,
    query,
  ]);

  useEffect(() => {
    setSelectedIndex((prev) =>
      items.length === 0 ? 0 : Math.min(prev, items.length - 1),
    );
  }, [items.length]);

  if (!open) return null;

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault();
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
      e.preventDefault();
      const target = items[selectedIndex];
      if (target) target.run();
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-[12vh]"
      style={{ backgroundColor: 'var(--bg-overlay)' }}
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Command Palette"
        className="w-full max-w-[540px] overflow-hidden rounded-[8px] border shadow-2xl"
        style={{
          backgroundColor: 'var(--bg-surface)',
          borderColor: 'var(--border-strong)',
        }}
        onClick={(e) => e.stopPropagation()}
        onKeyDown={handleKeyDown}
      >
        <div
          className="flex items-center gap-2 border-b px-3 py-2.5"
          style={{ borderColor: 'var(--border-hairline)' }}
        >
          <Search
            className="h-4 w-4 shrink-0"
            style={{ color: 'var(--text-muted)' }}
          />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setSelectedIndex(0);
            }}
            placeholder="Search symbol (e.g. BTC, SOL), ticket, or command..."
            className="w-full bg-transparent text-[13px] outline-none"
            style={{ color: 'var(--text-primary)' }}
          />
          <span
            className="rounded border px-1.5 py-0.5 text-[10px]"
            style={{
              borderColor: 'var(--border-hairline)',
              color: 'var(--text-muted)',
            }}
          >
            ESC
          </span>
        </div>

        <div className="max-h-[380px] overflow-y-auto p-1.5">
          {items.length === 0 ? (
            <div
              className="px-3 py-6 text-center text-[12px]"
              style={{ color: 'var(--text-muted)' }}
            >
              No matching symbols, tickets, or commands.
            </div>
          ) : (
            items.map((item, idx) => {
              const active = idx === selectedIndex;
              return (
                <button
                  key={item.id}
                  type="button"
                  onMouseEnter={() => setSelectedIndex(idx)}
                  onClick={item.run}
                  className="flex w-full items-center justify-between gap-2 rounded-[6px] px-2.5 py-2 text-left transition-colors"
                  style={{
                    backgroundColor: active
                      ? 'var(--bg-elevated)'
                      : 'transparent',
                    color: 'var(--text-primary)',
                  }}
                >
                  <div className="flex min-w-0 items-center gap-2.5">
                    <span
                      style={{
                        color: active
                          ? 'var(--accent-brand)'
                          : 'var(--text-muted)',
                      }}
                    >
                      {item.icon}
                    </span>
                    <div className="min-w-0">
                      <div className="tabular-nums truncate text-[13px] font-medium">
                        {item.label}
                      </div>
                      {item.hint && (
                        <div
                          className="tabular-nums truncate text-[11px]"
                          style={{ color: 'var(--text-secondary)' }}
                        >
                          {item.hint}
                        </div>
                      )}
                    </div>
                  </div>
                  <span
                    className="shrink-0 text-[10px] uppercase tracking-wider"
                    style={{ color: 'var(--text-muted)' }}
                  >
                    {item.group}
                  </span>
                </button>
              );
            })
          )}
        </div>
      </div>
    </div>
  );
};
