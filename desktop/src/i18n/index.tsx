import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react';
import type React from 'react';
import { en } from './en';
import { zh, type TranslationKey } from './zh';

export type Locale = 'zh' | 'en';
export type TranslateParams = Record<string, string | number>;
export type TranslateFn = (key: TranslationKey, params?: TranslateParams) => string;
export type { TranslationKey } from './zh';

export const LOCALE_STORAGE_KEY = 'kairos.desk.locale';

const DICTIONARIES: Record<Locale, Readonly<Record<TranslationKey, string>>> = {
  zh,
  en,
};

function readStoredLocale(): Locale {
  if (typeof window === 'undefined') return 'zh';
  try {
    return window.localStorage.getItem(LOCALE_STORAGE_KEY) === 'en' ? 'en' : 'zh';
  } catch {
    return 'zh';
  }
}

function applyDocumentLang(locale: Locale): void {
  if (typeof document === 'undefined') return;
  document.documentElement.lang = locale === 'en' ? 'en' : 'zh-CN';
}

function formatMessage(template: string, params?: TranslateParams): string {
  if (!params) return template;
  return template.replace(/\{([^{}]+)\}/g, (match, name: string) => {
    if (Object.prototype.hasOwnProperty.call(params, name)) {
      return String(params[name]);
    }
    return match;
  });
}

let currentLocale: Locale = readStoredLocale();
applyDocumentLang(currentLocale);

const listeners = new Set<(locale: Locale) => void>();

export function getLocale(): Locale {
  return currentLocale;
}

export function setLocale(next: Locale): void {
  const normalized: Locale = next === 'en' ? 'en' : 'zh';
  if (typeof window !== 'undefined') {
    try {
      window.localStorage.setItem(LOCALE_STORAGE_KEY, normalized);
    } catch {
      // ignore storage errors
    }
  }
  applyDocumentLang(normalized);
  if (normalized === currentLocale) return;
  currentLocale = normalized;
  for (const listener of listeners) {
    listener(normalized);
  }
}

export function toggleLocale(): void {
  setLocale(currentLocale === 'zh' ? 'en' : 'zh');
}

export function t(key: TranslationKey, params?: TranslateParams): string {
  const dict = DICTIONARIES[currentLocale] ?? zh;
  const template = dict[key] ?? zh[key] ?? String(key);
  return formatMessage(template, params);
}

export interface I18nContextValue {
  locale: Locale;
  setLocale: (next: Locale) => void;
  toggleLocale: () => void;
  t: TranslateFn;
}

const I18nContext = createContext<I18nContextValue | null>(null);

export function I18nProvider(props: { children: React.ReactNode }): React.JSX.Element {
  const [locale, setLocaleState] = useState<Locale>(() => getLocale());

  useEffect(() => {
    applyDocumentLang(locale);
  }, [locale]);

  useEffect(() => {
    const sync = (next: Locale) => {
      setLocaleState(next);
    };
    listeners.add(sync);
    const latest = getLocale();
    setLocaleState((prev) => (prev === latest ? prev : latest));
    return () => {
      listeners.delete(sync);
    };
  }, []);

  const translate = useMemo<TranslateFn>(() => {
    const dict = DICTIONARIES[locale] ?? zh;
    return (key: TranslationKey, params?: TranslateParams): string => {
      const template = dict[key] ?? zh[key] ?? String(key);
      return formatMessage(template, params);
    };
  }, [locale]);

  const handleSetLocale = useCallback((next: Locale) => {
    setLocale(next);
  }, []);

  const handleToggleLocale = useCallback(() => {
    toggleLocale();
  }, []);

  const value = useMemo<I18nContextValue>(
    () => ({
      locale,
      setLocale: handleSetLocale,
      toggleLocale: handleToggleLocale,
      t: translate,
    }),
    [locale, handleSetLocale, handleToggleLocale, translate],
  );

  return <I18nContext.Provider value={value}>{props.children}</I18nContext.Provider>;
}

export function useI18n(): I18nContextValue {
  const ctx = useContext(I18nContext);
  return (
    ctx ?? {
      locale: getLocale(),
      setLocale,
      toggleLocale,
      t,
    }
  );
}
