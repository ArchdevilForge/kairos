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
import { useI18n } from '../i18n';
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
  const { locale, toggleLocale, t } = useI18n();
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
    { id: 'watch', label: t('top.tab.watch') },
    { id: 'review', label: t('top.tab.review') },
    { id: 'stats', label: t('top.tab.stats') },
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
      text: t('top.pulse.lockedText'),
      color: 'var(--pnl-neg)',
      bg: 'var(--candle-bear-soft)',
      title: t('top.pulse.lockedTitle', { symbols: lockedList.join(', ') }),
    };
  } else if (cooldownCount > 0) {
    alertPill = {
      text: t('top.pulse.cooldownText'),
      color: 'var(--state-warn)',
      bg: 'var(--state-warn-soft)',
      title: t('top.pulse.cooldownTitle', { count: cooldownCount }),
    };
  } else if (hasActivePulse) {
    const med60 =
      pulse.median_return_60s ?? pulse.snapshot?.median_return_60s ?? 0;
    const rawBreadth = isDownPulse
      ? (pulse.down_breadth_60s ?? pulse.snapshot?.down_breadth_60s ?? 0)
      : (pulse.up_breadth_60s ?? pulse.snapshot?.up_breadth_60s ?? 0);
    const breadthPct = Math.round(rawBreadth * 100);
    const regimePrefix = isStressPulse
      ? t('top.pulse.regimeStress')
      : isDownPulse
        ? t('top.pulse.regimeDown')
        : t('top.pulse.regimeUp');
    const med = `${med60 >= 0 ? '+' : ''}${med60.toFixed(2)}`;
    const side = isDownPulse ? t('top.pulse.sideBear') : t('top.pulse.sideBull');
    alertPill = {
      text: t('top.pulse.activeText', { regime: regimePrefix, breadth: breadthPct }),
      color: isDownPulse ? 'var(--pnl-neg)' : 'var(--pnl-pos)',
      bg: isDownPulse ? 'var(--candle-bear-soft)' : 'var(--candle-bull-soft)',
      title: t('top.pulse.activeTitle', { med, side, breadth: breadthPct }),
    };
  } else {
    alertPill = {
      text: t('top.pulse.quietText'),
      color: 'var(--text-muted)',
      bg: 'transparent',
      title:
        pulse?.state === 'DECAY'
          ? t('top.pulse.decayTitle')
          : t('top.pulse.quietTitle'),
      quiet: true,
    };
  }

  return (
    <header
      className="flex min-h-[38px] shrink-0 items-center justify-between gap-x-1.5 gap-y-1 border-b px-2 py-1 select-none max-[359px]:flex-wrap sm:gap-x-2 sm:px-3 pointer-coarse:max-[419px]:flex-wrap"
      style={{
        backgroundColor: 'var(--bg-surface)',
        borderColor: 'var(--border-hairline)',
      }}
    >
      <div className="flex min-w-0 items-center gap-1.5 max-[359px]:basis-full sm:gap-2.5 pointer-coarse:max-[419px]:basis-full">
        <div className="hidden items-center pr-0.5 min-[420px]:flex sm:pr-1">
          <span
            className="font-editorial text-[15px] font-medium tracking-tight"
            style={{ color: 'var(--text-primary)' }}
          >
            Kairos
          </span>
        </div>

        <nav
          aria-label={t('top.workspaceSwitcherAria')}
          className="flex shrink-0 items-center rounded-[6px] border p-0.5"
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
                aria-current={active ? 'page' : undefined}
                className="min-h-6 shrink-0 whitespace-nowrap rounded-[4px] px-2 py-0.5 text-[12px] font-medium transition-colors sm:px-2.5 pointer-coarse:min-h-11 pointer-coarse:min-w-11"
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
          className="flex min-h-6 min-w-0 items-center gap-1.5 rounded-[6px] border px-2 py-0.5 text-[12px] transition-colors pointer-coarse:min-h-11"
          style={{
            backgroundColor: 'var(--bg-canvas)',
            borderColor: 'var(--border-hairline)',
            color: 'var(--text-primary)',
          }}
          title={t('top.commandPaletteTitle')}
        >
          <span className="tabular-nums truncate font-medium">
            {cleanSymbol(activeSymbol)}
          </span>
          <span
            className="hidden shrink-0 items-center gap-0.5 rounded px-1 text-[10px] sm:flex"
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
      <div className="hidden min-w-0 items-center justify-center gap-1.5 md:flex">
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
          <span className="whitespace-nowrap">{alertPill.text}</span>
        </div>

        {moverChips.length > 0 ? (
          <div className="hidden min-w-0 items-center gap-1 lg:flex">
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
                  title={t('top.moverChipTitle', {
                    symbol: cleanSymbol(chip.symbol),
                    rel: `${chip.relative_pct >= 0 ? '+' : ''}${Number(chip.relative_pct ?? 0).toFixed(2)}`,
                  })}
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

      {/* Right: Language, Theme & Settings */}
      <div className="flex shrink-0 items-center gap-1">
        <span className="px-1 font-mono text-[10px]" style={{ color: 'var(--text-secondary)' }} translate="no">
          {status.exchange.toUpperCase()}
        </span>
        <button
          type="button"
          onClick={toggleLocale}
          className="flex h-7 items-center gap-0.5 rounded-[6px] border px-1.5 text-[11px] font-medium transition-colors hover:opacity-85 pointer-coarse:min-h-11 pointer-coarse:min-w-11 pointer-coarse:justify-center"
          style={{
            backgroundColor: 'var(--bg-canvas)',
            borderColor: 'var(--border-hairline)',
          }}
          title={t('top.langToggleTitle')}
          aria-label={t('top.langToggleAria')}
        >
          <span
            style={{
              color: locale === 'zh' ? 'var(--text-primary)' : 'var(--text-secondary)',
            }}
          >
            中
          </span>
          <span style={{ color: 'var(--text-muted)' }}>/</span>
          <span
            style={{
              color: locale === 'en' ? 'var(--text-primary)' : 'var(--text-secondary)',
            }}
          >
            EN
          </span>
        </button>

        <button
          type="button"
          onClick={onToggleTheme}
          className="flex h-7 w-7 items-center justify-center rounded-[6px] transition-colors hover:opacity-80 pointer-coarse:h-11 pointer-coarse:w-11"
          style={{ color: 'var(--text-secondary)' }}
          title={theme === 'dark' ? t('top.themeToLight') : t('top.themeToDark')}
          aria-label={t('top.themeAria')}
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
          className="flex h-7 w-7 items-center justify-center rounded-[6px] transition-colors hover:opacity-80 pointer-coarse:h-11 pointer-coarse:w-11"
          style={{ color: 'var(--text-secondary)' }}
          title={t('top.settingsTitle')}
          aria-label={t('top.settingsAria')}
        >
          <Settings className="h-3.5 w-3.5" />
        </button>
      </div>
    </header>
  );
};
