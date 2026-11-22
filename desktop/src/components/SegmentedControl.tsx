import React, { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';

export interface SegmentedOption<T extends string> {
  value: T;
  label: React.ReactNode;
  title?: string;
  activeColor?: string;
}

export interface SegmentedControlProps<T extends string> {
  options: ReadonlyArray<SegmentedOption<T>>;
  value: T;
  onChange: (value: T) => void;
  ariaLabel?: string;
  variant?: 'tabs' | 'group';
  size?: 'xs' | 'sm';
  fullWidth?: boolean;
  className?: string;
  mono?: boolean;
}

/**
 * Linear-inspired segmented control with a GPU-accelerated sliding indicator pill.
 * Supports both proper ARIA tablist (roving tabindex + arrow keys) and pressed button group.
 */
export function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  ariaLabel,
  variant = 'group',
  size = 'sm',
  fullWidth = false,
  className = '',
  mono = false,
}: SegmentedControlProps<T>) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const itemRefs = useRef<Map<T, HTMLButtonElement>>(new Map());
  const hasMeasuredRef = useRef(false);
  const rafRef = useRef<number>(0);
  const [pill, setPill] = useState<{ left: number; width: number; ready: boolean }>({
    left: 0,
    width: 0,
    ready: false,
  });
  const [animatePill, setAnimatePill] = useState(false);

  const measure = useCallback(() => {
    const container = containerRef.current;
    const activeEl = itemRefs.current.get(value);
    if (!container || !activeEl) {
      setPill((prev) => (prev.ready ? { left: 0, width: 0, ready: false } : prev));
      return;
    }

    const nextLeft = activeEl.offsetLeft;
    const nextWidth = activeEl.offsetWidth;
    if (nextWidth <= 0) {
      setPill((prev) => (prev.ready ? { left: 0, width: 0, ready: false } : prev));
      return;
    }

    setPill((prev) => {
      if (prev.ready && prev.left === nextLeft && prev.width === nextWidth) {
        return prev;
      }
      return { left: nextLeft, width: nextWidth, ready: true };
    });

    if (!hasMeasuredRef.current) {
      hasMeasuredRef.current = true;
      if (rafRef.current) cancelAnimationFrame(rafRef.current);
      rafRef.current = requestAnimationFrame(() => {
        rafRef.current = 0;
        setAnimatePill(true);
      });
    }
  }, [value]);

  useLayoutEffect(() => {
    measure();
  }, [measure, options]);

  useEffect(() => {
    const container = containerRef.current;
    const activeEl = itemRefs.current.get(value);
    if (!container || typeof ResizeObserver === 'undefined') return;

    const ro = new ResizeObserver(() => measure());
    ro.observe(container);
    if (activeEl) ro.observe(activeEl);

    let cancelled = false;
    if (typeof document !== 'undefined' && document.fonts?.ready) {
      void document.fonts.ready.then(() => {
        if (!cancelled) measure();
      });
    }

    return () => {
      cancelled = true;
      ro.disconnect();
      if (rafRef.current) {
        cancelAnimationFrame(rafRef.current);
        rafRef.current = 0;
      }
    };
  }, [measure, value, options]);

  const handleKeyDown = (e: React.KeyboardEvent, idx: number) => {
    if (variant !== 'tabs' || options.length === 0) return;
    let nextIdx: number | null = null;
    if (e.key === 'ArrowRight') {
      nextIdx = (idx + 1) % options.length;
    } else if (e.key === 'ArrowLeft') {
      nextIdx = (idx - 1 + options.length) % options.length;
    } else if (e.key === 'Home') {
      nextIdx = 0;
    } else if (e.key === 'End') {
      nextIdx = options.length - 1;
    }
    if (nextIdx != null) {
      e.preventDefault();
      const targetOpt = options[nextIdx];
      onChange(targetOpt.value);
      itemRefs.current.get(targetOpt.value)?.focus();
    }
  };

  const padClass =
    size === 'xs'
      ? 'px-1.5 py-0.5 text-[11px]'
      : 'px-2 py-0.5 text-[12px] sm:px-2.5';

  return (
    <div
      ref={containerRef}
      role={variant === 'tabs' ? 'tablist' : 'group'}
      aria-label={ariaLabel}
      className={`relative inline-flex items-center rounded-[6px] border p-0.5 ${
        fullWidth ? 'flex w-full' : 'shrink-0'
      } ${className}`}
      style={{
        backgroundColor: 'var(--bg-canvas)',
        borderColor: 'var(--border-hairline)',
      }}
    >
      {pill.ready ? (
        <span
          aria-hidden="true"
          className="pointer-events-none absolute top-0.5 bottom-0.5 left-0 rounded-[4px]"
          style={{
            width: `${pill.width}px`,
            transform: `translate3d(${pill.left}px, 0, 0)`,
            backgroundColor: 'var(--bg-elevated)',
            boxShadow: 'var(--shadow-pill)',
            transition: animatePill
              ? 'transform var(--dur-fast) var(--ease-out-expo), width var(--dur-fast) var(--ease-out-expo)'
              : 'none',
          }}
        />
      ) : null}

      {options.map((opt, idx) => {
        const active = opt.value === value;
        const resolvedActiveColor =
          opt.activeColor === 'var(--accent-brand)'
            ? 'var(--accent-brand-text)'
            : (opt.activeColor ?? 'var(--text-primary)');
        return (
          <button
            key={opt.value}
            ref={(el) => {
              if (el) itemRefs.current.set(opt.value, el);
              else itemRefs.current.delete(opt.value);
            }}
            type="button"
            role={variant === 'tabs' ? 'tab' : undefined}
            aria-selected={variant === 'tabs' ? active : undefined}
            aria-pressed={variant === 'group' ? active : undefined}
            tabIndex={variant === 'tabs' ? (active ? 0 : -1) : 0}
            title={opt.title}
            onClick={() => onChange(opt.value)}
            onKeyDown={(e) => handleKeyDown(e, idx)}
            className={`relative z-[1] inline-flex items-center justify-center gap-1 whitespace-nowrap rounded-[4px] font-medium transition-colors duration-100 ${padClass} ${
              fullWidth ? 'flex-1' : 'shrink-0'
            } ${mono ? 'font-mono tabular-nums' : ''}`}
            style={{
              color: active ? resolvedActiveColor : 'var(--text-secondary)',
            }}
          >
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}
