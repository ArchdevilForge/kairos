import React, { useMemo } from 'react';
import {
  Activity,
  BarChart3,
  Eye,
  History,
  Keyboard,
  Moon,
  Search,
  Settings,
  ShieldAlert,
  Sun,
} from 'lucide-react';
import type { DeskStatus, SymbolMove } from '../types/kairos';
import { useI18n } from '../i18n';
import {
  formatExchangeLabel,
  resolveAccountExchange,
  sameSymbol,
} from '../utils/doctrine';
import { SegmentedControl, type SegmentedOption } from './SegmentedControl';

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
  onOpenShortcuts?: () => void;
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
  wsConnected = false,
  theme,
  onToggleTheme,
  onOpenCommandPalette,
  onOpenSettings,
  onOpenShortcuts,
  onSelectSymbol,
}) => {
  const { locale, toggleLocale, t } = useI18n();
  const activeExchange = resolveAccountExchange(status);
  const exchangeLabel = formatExchangeLabel(activeExchange, locale);
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

  const tabOptions = useMemo<ReadonlyArray<SegmentedOption<WorkspaceTab>>>(
    () => [
      {
        value: 'watch',
        label: (
          <>
            <Eye className="hidden h-3 w-3 sm:inline-block" />
            <span>{t('top.tab.watch')}</span>
          </>
        ),
      },
      {
        value: 'review',
        label: (
          <>
            <History className="hidden h-3 w-3 sm:inline-block" />
            <span>{t('top.tab.review')}</span>
          </>
        ),
      },
      {
        value: 'stats',
        label: (
          <>
            <BarChart3 className="hidden h-3 w-3 sm:inline-block" />
            <span>{t('top.tab.stats')}</span>
          </>
        ),
      },
    ],
    [t],
  );

  let alertPill: {
    text: string;
    shortText: string;
    color: string;
    bg: string;
    title: string;
    quiet?: boolean;
    live?: boolean;
  };
  if (lockedCount > 0) {
    alertPill = {
      text: t('top.pulse.lockedText'),
      shortText: t('top.pulse.lockedText'),
      color: 'var(--pnl-neg)',
      bg: 'var(--candle-bear-soft)',
      title: t('top.pulse.lockedTitle', { symbols: lockedList.join(', ') }),
      live: true,
    };
  } else if (cooldownCount > 0) {
    alertPill = {
      text: t('top.pulse.cooldownText'),
      shortText: t('top.pulse.cooldownText'),
      color: 'var(--state-warn)',
      bg: 'var(--state-warn-soft)',
      title: t('top.pulse.cooldownTitle', { count: cooldownCount }),
      live: true,
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
      shortText: `${breadthPct}%`,
      color: isDownPulse ? 'var(--pnl-neg)' : 'var(--pnl-pos)',
      bg: isDownPulse ? 'var(--candle-bear-soft)' : 'var(--candle-bull-soft)',
      title: t('top.pulse.activeTitle', { med, side, breadth: breadthPct }),
      live: true,
    };
  } else {
    alertPill = {
      text: t('top.pulse.quietText'),
      shortText: t('top.pulse.quietText'),
      color: 'var(--text-muted)',
      bg: 'transparent',
      title:
        pulse?.state === 'DECAY'
          ? t('top.pulse.decayTitle')
          : t('top.pulse.quietTitle'),
      quiet: true,
      live: false,
    };
  }

  return (
    <header
      className="flex h-[40px] shrink-0 items-center justify-between gap-1.5 border-b px-2 select-none sm:gap-2.5 sm:px-3"
      style={{
        backgroundColor: 'var(--bg-surface)',
        borderColor: 'var(--border-hairline)',
        boxShadow: 'var(--inset-highlight)',
      }}
    >
      {/* Left: Brand + WS Link + Sliding Workspace Switcher + Command Trigger */}
      <div className="flex min-w-0 items-center gap-1.5 sm:gap-2.5">
        <div
          className="hidden items-center gap-2 pr-0.5 min-[440px]:flex sm:pr-1"
          title={wsConnected ? t('top.wsConnectedTitle') : t('top.wsDisconnectedTitle')}
        >
          <span className="relative flex h-2 w-2 shrink-0 items-center justify-center">
            {!wsConnected ? (
              <span
                className="kairos-pulse-ring absolute inline-flex h-full w-full rounded-full"
                style={{ backgroundColor: 'var(--state-warn)' }}
              />
            ) : null}
            <span
              className="relative inline-flex h-2 w-2 rounded-full transition-colors duration-300"
              style={{
                backgroundColor: wsConnected ? 'var(--pnl-pos)' : 'var(--state-warn)',
              }}
            />
          </span>
          <span
            className="font-editorial text-[15px] font-medium tracking-tight"
            style={{ color: 'var(--text-primary)' }}
          >
            Kairos
          </span>
        </div>

        <SegmentedControl<WorkspaceTab>
          options={tabOptions}
          value={activeView}
          onChange={onChangeView}
          ariaLabel={t('top.workspaceSwitcherAria')}
          variant="tabs"
          size="sm"
        />

        <button
          type="button"
          onClick={onOpenCommandPalette}
          className="kairos-btn flex h-[27px] min-w-0 items-center gap-1.5 rounded-[6px] border px-2 text-[12px]"
          style={{
            backgroundColor: 'var(--bg-canvas)',
            borderColor: 'var(--border-hairline)',
            color: 'var(--text-primary)',
          }}
          title={t('top.commandPaletteTitle')}
        >
          <Search
            className="hidden h-3 w-3 shrink-0 sm:inline-block"
            style={{ color: 'var(--text-muted)' }}
          />
          <span className="tabular-nums truncate font-medium">
            {cleanSymbol(activeSymbol)}
          </span>
          <kbd className="kairos-kbd hidden shrink-0 sm:inline-flex">⌘K</kbd>
        </button>
      </div>

      {/* Center: Compact pulse indicator on narrow viewports + full MarketPulse & Top-3 movers on md+ */}
      <div className="flex min-w-0 items-center justify-center gap-1.5">
        {/* Compact pulse badge for < md viewports when active or locked */}
        {alertPill.live ? (
          <div
            className="flex shrink-0 items-center gap-1 rounded-[6px] px-2 py-0.5 text-[11px] font-medium tabular-nums md:hidden"
            style={{
              backgroundColor: alertPill.bg,
              color: alertPill.color,
            }}
            title={alertPill.title}
          >
            <span className="relative flex h-1.5 w-1.5 shrink-0">
              <span
                className="kairos-pulse-ring absolute inline-flex h-full w-full rounded-full"
                style={{ backgroundColor: alertPill.color }}
              />
              <span
                className="relative inline-flex h-1.5 w-1.5 rounded-full"
                style={{ backgroundColor: alertPill.color }}
              />
            </span>
            <span className="whitespace-nowrap">{alertPill.shortText}</span>
          </div>
        ) : null}

        {/* Full pulse capsule on md+ */}
        <div
          className="hidden shrink-0 items-center gap-1.5 rounded-[6px] px-2.5 py-0.5 text-[11px] font-medium tabular-nums transition-colors duration-200 md:flex"
          style={{
            backgroundColor: alertPill.bg,
            color: alertPill.color,
            border: alertPill.quiet
              ? '1px solid var(--border-hairline)'
              : '1px solid transparent',
          }}
          title={alertPill.title}
        >
          {alertPill.live ? (
            <span className="relative flex h-2 w-2 shrink-0 items-center justify-center">
              <span
                className="kairos-pulse-ring absolute inline-flex h-full w-full rounded-full"
                style={{ backgroundColor: alertPill.color }}
              />
              <span
                className="relative inline-flex h-1.5 w-1.5 rounded-full"
                style={{ backgroundColor: alertPill.color }}
              />
            </span>
          ) : lockedCount > 0 || cooldownCount > 0 ? (
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
                  className="kairos-btn tabular-nums flex shrink-0 items-center gap-1 rounded-[5px] border px-1.5 py-0.5 text-[11px]"
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

      {/* Right: Language, Shortcuts, Theme & Settings */}
      <div className="flex shrink-0 items-center gap-1">
        <button
          type="button"
          onClick={toggleLocale}
          className="kairos-btn flex h-7 items-center gap-0.5 rounded-[6px] border px-1.5 text-[11px] font-medium"
          style={{
            backgroundColor: 'var(--bg-canvas)',
            borderColor: 'var(--border-hairline)',
          }}
          title={t('top.langToggleTitle')}
          aria-label={t('top.langToggleAria')}
        >
          <span
            style={{
              color: locale === 'zh' ? 'var(--text-primary)' : 'var(--text-muted)',
            }}
          >
            中
          </span>
          <span style={{ color: 'var(--border-strong)' }}>/</span>
          <span
            style={{
              color: locale === 'en' ? 'var(--text-primary)' : 'var(--text-muted)',
            }}
          >
            EN
          </span>
        </button>

        {onOpenShortcuts ? (
          <button
            type="button"
            onClick={onOpenShortcuts}
            className="kairos-btn hidden h-7 w-7 items-center justify-center rounded-[6px] border border-transparent sm:flex"
            style={{ color: 'var(--text-secondary)' }}
            title={t('top.shortcutsTitle')}
            aria-label={t('top.shortcutsAria')}
          >
            <Keyboard className="h-3.5 w-3.5" />
          </button>
        ) : null}

        <button
          type="button"
          onClick={onToggleTheme}
          className="kairos-btn flex h-7 w-7 items-center justify-center rounded-[6px] border border-transparent"
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
          className="kairos-btn flex h-7 w-7 items-center justify-center rounded-[6px] border border-transparent"
          style={{ color: 'var(--text-secondary)' }}
          title={t('top.settingsTitle', { exchange: exchangeLabel })}
          aria-label={t('top.settingsAria')}
        >
          <Settings className="h-3.5 w-3.5" />
        </button>
      </div>
    </header>
  );
};
