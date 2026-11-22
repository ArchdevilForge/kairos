import React, { useEffect, useRef } from 'react';
import { Keyboard, X } from 'lucide-react';
import { useI18n } from '../i18n';

export interface ShortcutsModalProps {
  open: boolean;
  onClose: () => void;
}

interface ShortcutRow {
  keys: string[];
  label: string;
}

interface ShortcutGroup {
  title: string;
  rows: ShortcutRow[];
}

export const ShortcutsModal: React.FC<ShortcutsModalProps> = ({ open, onClose }) => {
  const { t } = useI18n();
  const closeBtnRef = useRef<HTMLButtonElement | null>(null);
  const prevFocusRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!open) return;
    prevFocusRef.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const timer = window.setTimeout(() => closeBtnRef.current?.focus(), 10);
    return () => {
      window.clearTimeout(timer);
      const prev = prevFocusRef.current;
      prevFocusRef.current = null;
      if (prev && prev.isConnected) {
        requestAnimationFrame(() => prev.focus());
      }
    };
  }, [open]);

  if (!open) return null;

  const groups: ShortcutGroup[] = [
    {
      title: t('shortcuts.group.global'),
      rows: [
        { keys: ['⌘/Ctrl', 'K'], label: t('shortcuts.item.palette') },
        { keys: ['?'], label: t('shortcuts.item.help') },
        { keys: ['Esc'], label: t('shortcuts.item.escape') },
      ],
    },
    {
      title: t('shortcuts.group.watch'),
      rows: [
        { keys: ['1', '…', '5'], label: t('shortcuts.item.timeframes') },
        { keys: ['S'], label: t('shortcuts.item.split') },
        { keys: ['J', 'K'], label: t('shortcuts.item.queueNav') },
      ],
    },
    {
      title: t('shortcuts.group.chart'),
      rows: [
        { keys: ['Shift', 'Drag'], label: t('shortcuts.item.quickRuler') },
        { keys: ['Ctrl/⌘'], label: t('shortcuts.item.magnetInvert') },
        { keys: ['Del'], label: t('shortcuts.item.deleteDrawing') },
      ],
    },
    {
      title: t('shortcuts.group.review'),
      rows: [
        { keys: ['Space'], label: t('shortcuts.item.playPause') },
        { keys: ['←', '→'], label: t('shortcuts.item.stepCursor') },
        { keys: ['A', 'N'], label: t('shortcuts.item.jumpAnomaly') },
      ],
    },
  ];

  return (
    <div
      className="kairos-glass-backdrop kairos-animate-backdrop fixed inset-0 z-50 flex items-center justify-center p-3 sm:p-4"
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t('shortcuts.title')}
        className="kairos-floating kairos-animate-modal max-h-[85dvh] w-full max-w-[540px] overflow-y-auto rounded-[10px]"
        onClick={(e) => e.stopPropagation()}
      >
        <div
          className="flex items-center justify-between border-b px-4 py-3"
          style={{ borderColor: 'var(--border-hairline)' }}
        >
          <div className="flex items-center gap-2">
            <Keyboard className="h-4 w-4" style={{ color: 'var(--accent-brand-text)' }} />
            <div>
              <h2
                className="font-editorial text-[16px] font-medium leading-tight"
                style={{ color: 'var(--text-primary)' }}
              >
                {t('shortcuts.title')}
              </h2>
              <p className="text-[11px]" style={{ color: 'var(--text-secondary)' }}>
                {t('shortcuts.subtitle')}
              </p>
            </div>
          </div>
          <button
            ref={closeBtnRef}
            type="button"
            onClick={onClose}
            className="kairos-btn rounded-[6px] border border-transparent p-1"
            style={{ color: 'var(--text-muted)' }}
            aria-label={t('shortcuts.closeAria')}
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="grid grid-cols-1 gap-3 p-4 sm:grid-cols-2">
          {groups.map((group) => (
            <div
              key={group.title}
              className="rounded-[8px] border p-3"
              style={{
                backgroundColor: 'var(--bg-canvas)',
                borderColor: 'var(--border-hairline)',
              }}
            >
              <div
                className="mb-2 text-[10px] font-semibold uppercase tracking-wider"
                style={{ color: 'var(--text-muted)' }}
              >
                {group.title}
              </div>
              <div className="space-y-2">
                {group.rows.map((row) => (
                  <div
                    key={row.label}
                    className="flex items-center justify-between gap-2 text-[12px]"
                  >
                    <span
                      className="truncate"
                      style={{ color: 'var(--text-secondary)' }}
                    >
                      {row.label}
                    </span>
                    <span className="flex shrink-0 items-center gap-1">
                      {row.keys.map((k, idx) =>
                        k === '…' ? (
                          <span
                            key={idx}
                            className="text-[10px]"
                            style={{ color: 'var(--text-muted)' }}
                          >
                            –
                          </span>
                        ) : (
                          <kbd key={idx} className="kairos-kbd">
                            {k}
                          </kbd>
                        ),
                      )}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};
