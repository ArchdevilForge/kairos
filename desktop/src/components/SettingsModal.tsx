import React, { useEffect, useState } from 'react';
import { Bell, BellOff, Lock, RefreshCw, Save, X } from 'lucide-react';
import { useI18n } from '../i18n';
import type {
  AccountSyncResponse,
  DeskStatus,
  UpdateSettingsRequest,
} from '../types/kairos';

export interface SettingsModalProps {
  open: boolean;
  onClose: () => void;
  status: DeskStatus;
  chimeEnabled: boolean;
  onToggleChime: () => void;
  syncingAccount: boolean;
  onSyncAccount: (opts?: { rethrow?: boolean }) => Promise<AccountSyncResponse | undefined>;
  onSaveSettings: (
    req: UpdateSettingsRequest,
    syncAfter: boolean,
  ) => Promise<AccountSyncResponse | undefined>;
}

export const SettingsModal: React.FC<SettingsModalProps> = ({
  open,
  onClose,
  status,
  chimeEnabled,
  onToggleChime,
  syncingAccount,
  onSyncAccount,
  onSaveSettings,
}) => {
  const { locale, setLocale, t } = useI18n();
  const [bankroll, setBankroll] = useState(
    String(status.risk_config.trading_bankroll_usd || 40),
  );
  const [maxLoss, setMaxLoss] = useState(
    String(status.risk_config.max_loss_usd || 4),
  );
  const [timezone, setTimezone] = useState(status.timezone || 'Asia/Shanghai');
  const [apiKey, setApiKey] = useState('');
  const [apiSecret, setApiSecret] = useState('');
  const [saving, setSaving] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setBankroll(String(status.risk_config.trading_bankroll_usd || 40));
    setMaxLoss(String(status.risk_config.max_loss_usd || 4));
    setTimezone(status.timezone || 'Asia/Shanghai');
    setApiKey('');
    setApiSecret('');
    setErrorMsg(null);
    setSuccessMsg(null);
  }, [
    open,
    status.risk_config.max_loss_usd,
    status.risk_config.trading_bankroll_usd,
    status.timezone,
  ]);

  if (!open) return null;

  const handleApiKeyChange = (value: string) => {
    setErrorMsg(null);
    setSuccessMsg(null);
    if (apiSecret === '') {
      const parts = value.trim().split(/\s+/);
      if (parts.length === 2 && parts[0].length >= 16 && parts[1].length >= 16) {
        setApiKey(parts[0]);
        setApiSecret(parts[1]);
        return;
      }
    }
    setApiKey(value);
  };

  const buildSettingsRequest = (): UpdateSettingsRequest | null => {
    const parsedBankroll = Number(bankroll);
    const parsedMaxLoss = Number(maxLoss);
    if (!Number.isFinite(parsedBankroll) || parsedBankroll <= 0) {
      setErrorMsg(t('settings.err.bankrollPositive'));
      return null;
    }
    if (!Number.isFinite(parsedMaxLoss) || parsedMaxLoss <= 0) {
      setErrorMsg(t('settings.err.maxLossPositive'));
      return null;
    }
    const hasKey = Boolean(apiKey.trim());
    const hasSecret = Boolean(apiSecret.trim());
    if (!status.account.configured && hasKey !== hasSecret) {
      setErrorMsg(t('settings.err.keySecretRequired'));
      return null;
    }

    const req: UpdateSettingsRequest = {
      trading_bankroll_usd: parsedBankroll,
      max_loss_usd: parsedMaxLoss,
      timezone: timezone.trim() || 'Asia/Shanghai',
    };
    if (hasKey) {
      req.binance_readonly_api_key = apiKey.trim();
    }
    if (hasSecret) {
      req.binance_readonly_api_secret = apiSecret.trim();
    }
    return req;
  };

  const handleImmediateSync = async () => {
    setErrorMsg(null);
    setSuccessMsg(null);
    if (apiKey.trim() || apiSecret.trim()) {
      const req = buildSettingsRequest();
      if (!req) return;
      setSaving(true);
      try {
        const res = await onSaveSettings(req, true);
        setApiKey('');
        setApiSecret('');
        if (res?.account) {
          setSuccessMsg(
            t('settings.syncSuccessDetail', {
              balance: res.account.wallet_balance_usd.toFixed(2),
              trades: res.synced_trades,
              fills: res.synced_fills,
            }),
          );
        } else {
          setSuccessMsg(t('settings.syncSuccess'));
        }
      } catch (err) {
        setErrorMsg(err instanceof Error ? err.message : t('settings.err.syncFailed'));
      } finally {
        setSaving(false);
      }
      return;
    }

    try {
      const res = await onSyncAccount({ rethrow: true });
      if (res?.account) {
        setSuccessMsg(
          t('settings.syncSuccessDetail', {
            balance: res.account.wallet_balance_usd.toFixed(2),
            trades: res.synced_trades,
            fills: res.synced_fills,
          }),
        );
      } else {
        setSuccessMsg(t('settings.syncSuccess'));
      }
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : t('settings.err.syncFailed'));
    }
  };

  const submit = async (syncAfter: boolean) => {
    setSuccessMsg(null);
    const req = buildSettingsRequest();
    if (!req) return;

    setSaving(true);
    setErrorMsg(null);
    try {
      await onSaveSettings(req, syncAfter);
      onClose();
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : t('settings.err.saveFailed'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-3 sm:p-4"
      style={{ backgroundColor: 'var(--bg-overlay)' }}
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t('settings.title')}
        className="kairos-contain-panel max-h-[88dvh] w-full max-w-[460px] overflow-y-auto rounded-[8px] border shadow-2xl"
        style={{
          backgroundColor: 'var(--bg-surface)',
          borderColor: 'var(--border-strong)',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <div
          className="flex items-center justify-between border-b px-4 py-3"
          style={{ borderColor: 'var(--border-hairline)' }}
        >
          <div>
            <h2
              className="font-editorial text-[17px] font-medium"
              style={{ color: 'var(--text-primary)' }}
            >
              {t('settings.title')}
            </h2>
            <p className="text-[11px]" style={{ color: 'var(--text-secondary)' }}>
              {t('settings.subtitle')}
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded p-1 transition-colors"
            style={{ color: 'var(--text-muted)' }}
            aria-label={t('settings.closeAria')}
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="space-y-4 p-4">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <label className="block">
              <span
                className="mb-1 block text-[11px] font-medium"
                style={{ color: 'var(--text-secondary)' }}
              >
                {t('settings.bankrollLabel')}
              </span>
              <input
                type="number"
                step="1"
                min="1"
                value={bankroll}
                onChange={(e) => setBankroll(e.target.value)}
                className="tabular-nums w-full rounded-[6px] border px-2.5 py-1.5 text-[13px] outline-none"
                style={{
                  backgroundColor: 'var(--bg-canvas)',
                  borderColor: 'var(--border-hairline)',
                  color: 'var(--text-primary)',
                }}
              />
            </label>

            <label className="block">
              <span
                className="mb-1 block text-[11px] font-medium"
                style={{ color: 'var(--text-secondary)' }}
              >
                {t('settings.maxLossLabel')}
              </span>
              <input
                type="number"
                step="0.5"
                min="0.5"
                value={maxLoss}
                onChange={(e) => setMaxLoss(e.target.value)}
                className="tabular-nums w-full rounded-[6px] border px-2.5 py-1.5 text-[13px] outline-none"
                style={{
                  backgroundColor: 'var(--bg-canvas)',
                  borderColor: 'var(--border-hairline)',
                  color: 'var(--text-primary)',
                }}
              />
            </label>
          </div>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <label className="block">
              <span
                className="mb-1 block text-[11px] font-medium"
                style={{ color: 'var(--text-secondary)' }}
              >
                {t('settings.timezoneLabel')}
              </span>
              <input
                type="text"
                value={timezone}
                onChange={(e) => setTimezone(e.target.value)}
                placeholder="Asia/Shanghai"
                className="w-full rounded-[6px] border px-2.5 py-1.5 text-[12px] outline-none"
                style={{
                  backgroundColor: 'var(--bg-canvas)',
                  borderColor: 'var(--border-hairline)',
                  color: 'var(--text-primary)',
                }}
              />
            </label>

            <div className="flex flex-col justify-end">
              <button
                type="button"
                onClick={onToggleChime}
                className="flex h-[34px] items-center justify-between rounded-[6px] border px-2.5 text-[12px] transition-colors"
                style={{
                  backgroundColor: 'var(--bg-canvas)',
                  borderColor: 'var(--border-hairline)',
                  color: chimeEnabled ? 'var(--text-primary)' : 'var(--text-secondary)',
                }}
              >
                <span>{t('settings.chimeLabel')}</span>
                {chimeEnabled ? (
                  <Bell className="h-3.5 w-3.5" style={{ color: 'var(--accent-brand)' }} />
                ) : (
                  <BellOff className="h-3.5 w-3.5" style={{ color: 'var(--text-muted)' }} />
                )}
              </button>
            </div>
          </div>

          <div
            className="flex items-center justify-between rounded-[6px] border px-3 py-2 text-[12px]"
            style={{
              backgroundColor: 'var(--bg-canvas)',
              borderColor: 'var(--border-hairline)',
            }}
          >
            <span className="font-medium" style={{ color: 'var(--text-secondary)' }}>
              {t('settings.languageLabel')}
            </span>
            <div
              className="flex items-center rounded-[5px] border p-0.5 text-[11px] font-medium"
              style={{
                backgroundColor: 'var(--bg-surface)',
                borderColor: 'var(--border-hairline)',
              }}
            >
              <button
                type="button"
                onClick={() => setLocale('zh')}
                className="rounded-[4px] px-2 py-0.5 transition-colors"
                style={{
                  backgroundColor: locale === 'zh' ? 'var(--bg-elevated)' : 'transparent',
                  color: locale === 'zh' ? 'var(--text-primary)' : 'var(--text-secondary)',
                }}
              >
                中
              </button>
              <button
                type="button"
                onClick={() => setLocale('en')}
                className="rounded-[4px] px-2 py-0.5 transition-colors"
                style={{
                  backgroundColor: locale === 'en' ? 'var(--bg-elevated)' : 'transparent',
                  color: locale === 'en' ? 'var(--text-primary)' : 'var(--text-secondary)',
                }}
              >
                EN
              </button>
            </div>
          </div>

          <div
            className="rounded-[6px] border p-3"
            style={{
              backgroundColor: 'var(--bg-canvas)',
              borderColor: 'var(--border-hairline)',
            }}
          >
            <div className="mb-2 flex items-center justify-between text-[11px]">
              <span
                className="flex items-center gap-1.5 font-medium"
                style={{ color: 'var(--text-primary)' }}
              >
                <Lock className="h-3.5 w-3.5" style={{ color: 'var(--accent-info)' }} />
                {t('settings.apiSectionTitle')}
              </span>
              <span
                className="tabular-nums"
                style={{
                  color: status.account.configured
                    ? 'var(--pnl-pos)'
                    : 'var(--text-muted)',
                }}
              >
                {status.account.configured
                  ? t('settings.apiConfigured', {
                      masked: status.account.api_key_masked,
                      balance: status.account.wallet_balance_usd.toFixed(2),
                    })
                  : t('settings.apiNotConfigured')}
              </span>
            </div>

            <div className="space-y-2">
              <input
                type="text"
                value={apiKey}
                onChange={(e) => handleApiKeyChange(e.target.value)}
                placeholder={
                  status.account.configured
                    ? t('settings.apiKeyKeepPlaceholder', {
                        masked: status.account.api_key_masked,
                      })
                    : t('settings.apiKeyPlaceholder')
                }
                autoComplete="off"
                className="tabular-nums w-full rounded-[6px] border px-2.5 py-1.5 text-[12px] outline-none"
                style={{
                  backgroundColor: 'var(--bg-surface)',
                  borderColor: 'var(--border-hairline)',
                  color: 'var(--text-primary)',
                }}
              />

              <input
                type="password"
                value={apiSecret}
                onChange={(e) => {
                  setErrorMsg(null);
                  setSuccessMsg(null);
                  setApiSecret(e.target.value);
                }}
                placeholder={t('settings.apiSecretPlaceholder')}
                autoComplete="new-password"
                className="tabular-nums w-full rounded-[6px] border px-2.5 py-1.5 text-[12px] outline-none"
                style={{
                  backgroundColor: 'var(--bg-surface)',
                  borderColor: 'var(--border-hairline)',
                  color: 'var(--text-primary)',
                }}
              />
            </div>

            {status.account.sync_error && !errorMsg && (
              <p className="mt-2 text-[11px]" style={{ color: 'var(--pnl-neg)' }}>
                {t('settings.lastSyncError', { error: status.account.sync_error })}
              </p>
            )}

            <div className="mt-2.5 flex flex-wrap items-center justify-between gap-2">
              <p className="text-[10px] leading-relaxed" style={{ color: 'var(--text-muted)' }}>
                {t('settings.secretsPrefix')}
                <code>~/.config/kairos/secrets.env</code>
                {t('settings.secretsSuffix')}
              </p>
              <button
                type="button"
                disabled={syncingAccount || saving}
                onClick={() => void handleImmediateSync()}
                className="flex shrink-0 items-center gap-1 rounded border px-2 py-1 text-[11px] disabled:opacity-60"
                style={{
                  backgroundColor: 'var(--bg-surface)',
                  borderColor: 'var(--border-hairline)',
                  color: status.account.sync_error
                    ? 'var(--pnl-neg)'
                    : 'var(--text-primary)',
                }}
              >
                <RefreshCw
                  className={`h-3 w-3 ${syncingAccount || saving ? 'animate-spin' : ''}`}
                />
                <span>{t('settings.syncNow')}</span>
              </button>
            </div>
          </div>

          <div
            className="flex items-center justify-between rounded-[6px] border px-3 py-2 text-[11px]"
            style={{
              backgroundColor: 'var(--bg-canvas)',
              borderColor: 'var(--border-hairline)',
              color: 'var(--text-secondary)',
            }}
          >
            <span>{t('settings.riskSummaryLabel')}</span>
            <span className="tabular-nums">
              {t('settings.riskSummaryValue', {
                major: status.risk_config.max_major_leverage,
                alt: status.risk_config.max_alt_leverage,
                cooldown: status.risk_config.cooldown_minutes,
              })}
            </span>
          </div>

          {successMsg && (
            <div
              className="tabular-nums rounded-[6px] border px-3 py-2 text-[12px]"
              style={{
                backgroundColor: 'var(--candle-bull-soft)',
                borderColor: 'var(--candle-bull)',
                color: 'var(--pnl-pos)',
              }}
            >
              {successMsg}
            </div>
          )}

          {errorMsg && (
            <div
              className="rounded-[6px] border px-3 py-2 text-[12px]"
              style={{
                backgroundColor: 'var(--candle-bear-soft)',
                borderColor: 'var(--candle-bear)',
                color: 'var(--pnl-neg)',
              }}
            >
              {errorMsg}
            </div>
          )}
        </div>

        <div
          className="flex flex-wrap items-center justify-end gap-2 border-t px-4 py-3"
          style={{ borderColor: 'var(--border-hairline)' }}
        >
          <button
            type="button"
            onClick={onClose}
            className="rounded-[6px] border px-3 py-1.5 text-[12px] font-medium"
            style={{
              backgroundColor: 'var(--bg-canvas)',
              borderColor: 'var(--border-hairline)',
              color: 'var(--text-secondary)',
            }}
          >
            {t('settings.cancel')}
          </button>

          <button
            type="button"
            disabled={saving}
            onClick={() => void submit(false)}
            className="flex items-center gap-1.5 rounded-[6px] border px-3 py-1.5 text-[12px] font-medium disabled:opacity-60"
            style={{
              backgroundColor: 'var(--bg-elevated)',
              borderColor: 'var(--border-strong)',
              color: 'var(--text-primary)',
            }}
          >
            <Save className="h-3.5 w-3.5" />
            {t('settings.save')}
          </button>

          <button
            type="button"
            disabled={saving}
            onClick={() => void submit(true)}
            className="flex items-center gap-1.5 rounded-[6px] px-3 py-1.5 text-[12px] font-medium disabled:opacity-60"
            style={{
              backgroundColor: 'var(--accent-brand)',
              color: '#FAF9F5',
            }}
          >
            <RefreshCw className={`h-3.5 w-3.5 ${saving ? 'animate-spin' : ''}`} />
            {t('settings.saveAndSync')}
          </button>
        </div>
      </div>
    </div>
  );
};
