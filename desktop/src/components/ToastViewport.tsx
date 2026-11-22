import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { AlertTriangle, CheckCircle2, Info, X, XCircle } from 'lucide-react';

export type ToastTone = 'info' | 'success' | 'warn' | 'error';

export interface ToastInput {
  id?: string;
  tone?: ToastTone;
  title: string;
  description?: string;
  durationMs?: number;
}

interface ToastRecord {
  id: string;
  version: number;
  tone: ToastTone;
  title: string;
  description?: string;
  durationMs: number;
}

interface ToastContextValue {
  pushToast: (toast: ToastInput) => void;
  dismissToast: (id: string) => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

const MAX_TOASTS = 3;

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<ToastRecord[]>([]);
  const seqRef = useRef(0);

  const dismissToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((item) => item.id !== id));
  }, []);

  const pushToast = useCallback((input: ToastInput) => {
    const version = ++seqRef.current;
    const id = input.id ?? `toast-${Date.now()}-${version}`;
    const record: ToastRecord = {
      id,
      version,
      tone: input.tone ?? 'info',
      title: input.title,
      description: input.description,
      durationMs: input.durationMs ?? (input.tone === 'error' ? 5200 : 3200),
    };
    setToasts((prev) => {
      const deduped = prev.filter((t) => t.id !== id);
      return [...deduped, record].slice(-MAX_TOASTS);
    });
  }, []);

  const value = useMemo(
    () => ({ pushToast, dismissToast }),
    [pushToast, dismissToast],
  );

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div
        aria-live="polite"
        aria-atomic="false"
        className="pointer-events-none fixed right-3 bottom-3 z-[60] flex max-h-[45dvh] w-[min(92vw,340px)] flex-col gap-2 overflow-y-auto p-0.5"
      >
        {toasts.map((toast) => (
          <ToastCard
            key={`${toast.id}:${toast.version}`}
            toast={toast}
            onDismiss={dismissToast}
          />
        ))}
      </div>
    </ToastContext.Provider>
  );
}

function ToastCard({
  toast,
  onDismiss,
}: {
  toast: ToastRecord;
  onDismiss: (id: string) => void;
}) {
  const [paused, setPaused] = useState(false);
  const remainingRef = useRef<number>(toast.durationMs);
  const startedAtRef = useRef<number>(Date.now());

  useEffect(() => {
    if (toast.durationMs <= 0) return;
    if (paused) {
      const elapsed = Date.now() - startedAtRef.current;
      remainingRef.current = Math.max(250, remainingRef.current - elapsed);
      return;
    }
    startedAtRef.current = Date.now();
    const timer = window.setTimeout(() => onDismiss(toast.id), remainingRef.current);
    return () => window.clearTimeout(timer);
  }, [paused, toast.durationMs, toast.id, toast.version, onDismiss]);

  const accentColor =
    toast.tone === 'success'
      ? 'var(--pnl-pos)'
      : toast.tone === 'warn'
        ? 'var(--state-warn)'
        : toast.tone === 'error'
          ? 'var(--pnl-neg)'
          : 'var(--accent-brand-text)';

  const Icon =
    toast.tone === 'success'
      ? CheckCircle2
      : toast.tone === 'warn'
        ? AlertTriangle
        : toast.tone === 'error'
          ? XCircle
          : Info;

  return (
    <div
      role={toast.tone === 'error' ? 'alert' : 'status'}
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocusCapture={() => setPaused(true)}
      onBlurCapture={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
          setPaused(false);
        }
      }}
      className="kairos-floating kairos-animate-toast pointer-events-auto relative flex shrink-0 items-start gap-2.5 overflow-hidden rounded-[8px] px-3 py-2.5"
    >
      <span
        aria-hidden="true"
        className="absolute top-0 bottom-0 left-0 w-[2.5px]"
        style={{ backgroundColor: accentColor }}
      />
      <Icon
        className="mt-0.5 h-3.5 w-3.5 shrink-0"
        style={{ color: accentColor }}
      />
      <div className="min-w-0 flex-1">
        <div
          className="text-[12px] font-medium leading-snug"
          style={{ color: 'var(--text-primary)' }}
        >
          {toast.title}
        </div>
        {toast.description ? (
          <div
            className="mt-0.5 line-clamp-3 break-words text-[11px] leading-relaxed tabular-nums"
            style={{ color: 'var(--text-secondary)' }}
          >
            {toast.description}
          </div>
        ) : null}
      </div>
      <button
        type="button"
        onClick={() => onDismiss(toast.id)}
        className="kairos-press rounded p-0.5 transition-colors hover:opacity-80"
        style={{ color: 'var(--text-muted)' }}
        aria-label="Dismiss notification"
      >
        <X className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}

const NOOP_TOAST: ToastContextValue = {
  pushToast: () => {},
  dismissToast: () => {},
};

export function useToast(): ToastContextValue {
  return useContext(ToastContext) ?? NOOP_TOAST;
}
