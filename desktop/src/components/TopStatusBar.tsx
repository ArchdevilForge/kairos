import React from 'react';
import {
  Activity,
  Command,
  Moon,
  Settings,
  ShieldAlert,
  Sun,
} from 'lucide-react';
import type { DeskStatus, SymbolMove } from '../types/kairos';
import { sameSymbol } from '../utils/doctrine';

export type WorkspaceTab = 'watch' | 'review' | 'stats';

export interface TopStatusBarProps {
  activeView: WorkspaceTab;
  onChangeView: (view: WorkspaceTab) => void;
  status: DeskStatus;
  activeSymbol: string;
  wsConnected?: boolean;
  theme: 'dark' | 'light';
  onToggleTheme: () => void;
  onOpenCommandPalette: () => void;
  onOpenSettings: () => void;
  onSelectSymbol?: (symbol: string) => void;
}

function cleanSymbol(sym: string): string {
  return sym.replace(/:USDT$/, '');
}

function shortTicker(sym: string): string {
  return sym.replace(/\/USDT(:USDT)?$/i, '').replace(/:USDT$/i, '');
}

export const TopStatusBar: React.FC<TopStatusBarProps> = ({
  activeView,
  onChangeView,
  status,
  activeSymbol,
  theme,
  onToggleTheme,
  onOpenCommandPalette,
  onOpenSettings,
  onSelectSymbol,
}) => {
  const pulse = status.pulse;
  const lockedList = status.gate_health?.locked_24h_symbols ?? [];
  const cooldownList = status.gate_health?.active_cooldowns ?? [];
  const lockedCount = lockedList.length;
  const cooldownCount = cooldownList.length;
  const hasActivePulse = Boolean(
    pulse?.state && pulse.state !== 'QUIET' && pulse.state !== 'DECAY',
  );
  const isDownPulse = Boolean(pulse?.state?.includes('DOWN'));
  const isStressPulse = Boolean(pulse?.state?.startsWith('STRESS'));

  const rawMovers = isDownPulse
    ? pulse?.snapshot?.laggards
    : pulse?.snapshot?.leaders;
  const moverChips: SymbolMove[] =
    hasActivePulse && Array.isArray(rawMovers)
      ? rawMovers
          .filter((m) => Boolean(m?.symbol) && m.symbol.toUpperCase() !== 'MARKET')
          .slice(0, 3)
      : [];

  const tabs: Array<{ id: WorkspaceTab; label: string }> = [
    { id: 'watch', label: '看盘' },
    { id: 'review', label: '复盘' },
    { id: 'stats', label: '统计' },
  ];

  let alertPill: {
    text: string;
    color: string;
    bg: string;
    title: string;
    quiet?: boolean;
  };
  if (lockedCount > 0) {
    alertPill = {
      text: '24h 锁仓冷却',
      color: 'var(--pnl-neg)',
      bg: 'var(--candle-bear-soft)',
      title: `24h 刚性锁仓: ${lockedList.join(', ')}`,
    };
  } else if (cooldownCount > 0) {
    alertPill = {
      text: '冷却中',
      color: 'var(--state-warn)',
      bg: 'var(--state-warn-soft)',
      title: `${cooldownCount} 个标的处于同币亏损冷却期`,
    };
  } else if (hasActivePulse) {
    const med60 =
      pulse.median_return_60s ?? pulse.snapshot?.median_return_60s ?? 0;
    const rawBreadth = isDownPulse
      ? (pulse.down_breadth_60s ?? pulse.snapshot?.down_breadth_60s ?? 0)
      : (pulse.up_breadth_60s ?? pulse.snapshot?.up_breadth_60s ?? 0);
    const breadthPct = Math.round(rawBreadth * 100);
    const regimePrefix = isStressPulse
      ? '极端共振'
      : isDownPulse
        ? '大盘下杀'
        : '大盘上攻';
    alertPill = {
      text: `${regimePrefix} · 广度 ${breadthPct}%`,
      color: isDownPulse ? 'var(--pnl-neg)' : 'var(--pnl-pos)',
      bg: isDownPulse ? 'var(--candle-bear-soft)' : 'var(--candle-bull-soft)',
      title: `60s 中位涨跌 ${med60 >= 0 ? '+' : ''}${med60.toFixed(2)}% · ${isDownPulse ? '空头' : '多头'}广度 ${breadthPct}%`,
    };
  } else {
    alertPill = {
      text: '大盘安静',
      color: 'var(--text-muted)',
      bg: 'transparent',
      title:
        pulse?.state === 'DECAY'
          ? '大盘脉冲衰减，回归安静观察'
          : '当前大盘安静，仅保留极值异动提示',
      quiet: true,
    };
  }

  return (
    <header
      className="flex h-[38px] shrink-0 items-center justify-between gap-2 border-b px-3 select-none"
      style={{
        backgroundColor: 'var(--bg-surface)',
        borderColor: 'var(--border-hairline)',
      }}
    >
      <div className="flex min-w-0 items-center gap-2.5">
        <div className="flex items-center pr-1">
          <span
            className="font-editorial text-[15px] font-medium tracking-tight"
            style={{ color: 'var(--text-primary)' }}
          >
            Kairos
          </span>
        </div>

        <nav
          aria-label="Workspace switcher"
          className="flex items-center rounded-[6px] border p-0.5"
          style={{
            backgroundColor: 'var(--bg-canvas)',
            borderColor: 'var(--border-hairline)',
          }}
        >
          {tabs.map((tab) => {
            const active = activeView === tab.id;
            return (
              <button
                key={tab.id}
                type="button"
                onClick={() => onChangeView(tab.id)}
                className="rounded-[4px] px-2.5 py-0.5 text-[12px] font-medium transition-colors"
                style={{
                  backgroundColor: active ? 'var(--bg-elevated)' : 'transparent',
                  color: active ? 'var(--text-primary)' : 'var(--text-secondary)',
                }}
              >
                {tab.label}
              </button>
            );
          })}
        </nav>

        <button
          type="button"
          onClick={onOpenCommandPalette}
          className="flex items-center gap-1.5 rounded-[6px] border px-2 py-0.5 text-[12px] transition-colors"
          style={{
            backgroundColor: 'var(--bg-canvas)',
            borderColor: 'var(--border-hairline)',
            color: 'var(--text-primary)',
          }}
          title="切换标的或快捷指令 (Ctrl/Cmd+K)"
        >
          <span className="tabular-nums font-medium">
            {cleanSymbol(activeSymbol)}
          </span>
          <span
            className="flex items-center gap-0.5 rounded px-1 text-[10px]"
            style={{
              backgroundColor: 'var(--bg-elevated)',
              color: 'var(--text-muted)',
            }}
          >
            <Command className="h-2.5 w-2.5" />K
          </span>
        </button>
      </div>

      {/* Center: Quiet breathing space or active MarketPulse + Top-3 Leaders/Laggards */}
      <div className="flex min-w-0 items-center justify-center gap-1.5">
        <div
          className="flex shrink-0 items-center gap-1.5 rounded-[6px] px-2.5 py-0.5 text-[11px] font-medium tabular-nums"
          style={{
            backgroundColor: alertPill.bg,
            color: alertPill.color,
            border: alertPill.quiet ? '1px solid var(--border-hairline)' : undefined,
          }}
          title={alertPill.title}
        >
          {lockedCount > 0 || cooldownCount > 0 ? (
            <ShieldAlert className="h-3 w-3 shrink-0" />
          ) : (
            <Activity className="h-3 w-3 shrink-0" />
          )}
          <span>{alertPill.text}</span>
        </div>

        {moverChips.length > 0 ? (
          <div className="flex min-w-0 items-center gap-1">
            {moverChips.map((chip) => {
              const ret = Number(chip.return_pct ?? 0);
              const isSelected = sameSymbol(chip.symbol, activeSymbol);
              return (
                <button
                  key={chip.symbol}
                  type="button"
                  onClick={() => onSelectSymbol?.(chip.symbol)}
                  className="tabular-nums flex shrink-0 items-center gap-1 rounded-[5px] border px-1.5 py-0.5 text-[11px] transition-colors hover:opacity-85"
                  style={{
                    backgroundColor: isSelected ? 'var(--bg-elevated)' : 'var(--bg-canvas)',
                    borderColor: isSelected ? 'var(--accent-brand)' : 'var(--border-hairline)',
                    color: 'var(--text-primary)',
                  }}
                  title={`切换主图至 ${cleanSymbol(chip.symbol)} (相对超额 ${chip.relative_pct >= 0 ? '+' : ''}${Number(chip.relative_pct ?? 0).toFixed(2)}%)`}
                >
                  <span className="font-medium">{shortTicker(chip.symbol)}</span>
                  <span style={{ color: ret >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)' }}>
                    {ret >= 0 ? '+' : ''}
                    {ret.toFixed(2)}%
                  </span>
                </button>
              );
            })}
          </div>
        ) : null}
      </div>

      {/* Right: Only Theme & Settings */}
      <div className="flex items-center gap-1">
        <button
          type="button"
          onClick={onToggleTheme}
          className="flex h-7 w-7 items-center justify-center rounded-[6px] transition-colors hover:opacity-80"
          style={{ color: 'var(--text-secondary)' }}
          title={theme === 'dark' ? '切换 Warm Parchment 亮色' : '切换 Warm Obsidian 暗色'}
          aria-label="切换主题"
        >
          {theme === 'dark' ? (
            <Sun className="h-3.5 w-3.5" />
          ) : (
            <Moon className="h-3.5 w-3.5" />
          )}
        </button>

        <button
          type="button"
          onClick={onOpenSettings}
          className="flex h-7 w-7 items-center justify-center rounded-[6px] transition-colors hover:opacity-80"
          style={{ color: 'var(--text-secondary)' }}
          title="风险预算与只读账户设置"
          aria-label="打开设置"
        >
          <Settings className="h-3.5 w-3.5" />
        </button>
      </div>
    </header>
  );
};
