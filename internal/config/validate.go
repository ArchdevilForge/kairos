package config

import (
	"fmt"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// knownExchanges are the exchange ids with real adapters.
var knownExchanges = map[string]bool{
	"okx":     true,
	"binance": true,
	"bybit":   true,
}

// Normalize resolves the legacy top-level `exchange` alias into
// exchanges.primary and dataManager.exchanges so the rest of the codebase has
// exactly two authorities: exchanges.primary (scanner/MarketPulse/provenance)
// and dataManager.exchanges (realtime adapter set).
func Normalize(cfg *types.Config) {
	if cfg.Exchanges.Primary == "" {
		if cfg.Exchange != "" {
			cfg.Exchanges.Primary = cfg.Exchange
		} else {
			cfg.Exchanges.Primary = "okx"
		}
	}
	if len(cfg.DataManager.Exchanges) == 0 {
		cfg.DataManager.Exchanges = []string{cfg.Exchanges.Primary}
	}
	// Keep the legacy field readable for old call sites and logs.
	cfg.Exchange = cfg.Exchanges.Primary
}

// Validate checks that the configuration is executable: no value may cause a
// panic, a silently-dead feature, or conflicting authorities at runtime. All
// violations are reported together.
func Validate(cfg *types.Config) error {
	var errs []string
	addf := func(format string, args ...any) {
		errs = append(errs, fmt.Sprintf(format, args...))
	}

	// --- exchange authorities -------------------------------------------------
	if !knownExchanges[cfg.Exchanges.Primary] {
		addf("exchanges.primary %q is not a supported exchange (okx|binance|bybit)", cfg.Exchanges.Primary)
	}
	seen := map[string]bool{}
	primaryInRealtime := false
	for _, ex := range cfg.DataManager.Exchanges {
		if !knownExchanges[ex] {
			addf("dataManager.exchanges contains unsupported exchange %q", ex)
		}
		if seen[ex] {
			addf("dataManager.exchanges lists %q more than once", ex)
		}
		seen[ex] = true
		if ex == cfg.Exchanges.Primary {
			primaryInRealtime = true
		}
	}
	if !primaryInRealtime {
		addf("exchanges.primary %q must be included in dataManager.exchanges %v (MarketPulse/scanner would otherwise run on an exchange with no realtime data)",
			cfg.Exchanges.Primary, cfg.DataManager.Exchanges)
	}
	for _, ex := range cfg.Exchanges.Backups {
		if !knownExchanges[ex] {
			addf("exchanges.backups contains unsupported exchange %q", ex)
		}
	}

	// --- dataManager ----------------------------------------------------------
	if cfg.DataManager.TopSymbols <= 0 {
		addf("dataManager.topSymbols must be > 0, got %d", cfg.DataManager.TopSymbols)
	}
	if cfg.DataManager.DedupWindowSeconds < 0 {
		addf("dataManager.dedupWindowSeconds must be >= 0, got %d", cfg.DataManager.DedupWindowSeconds)
	}
	if cfg.DataManager.SymbolCooldownMinutes < 0 {
		addf("dataManager.symbolCooldownMinutes must be >= 0, got %d", cfg.DataManager.SymbolCooldownMinutes)
	}
	if cfg.DataManager.RefreshIntervalHours < 0 {
		addf("dataManager.refreshIntervalHours must be >= 0, got %d", cfg.DataManager.RefreshIntervalHours)
	}

	// --- scanner --------------------------------------------------------------
	required := []string{"1d", "4h", "15m"}
	have := map[string]bool{}
	for _, tf := range cfg.Scanner.Timeframes {
		have[tf] = true
	}
	for _, tf := range required {
		if !have[tf] {
			addf("scanner.timeframes must include %q (analyzer dereferences 1d/4h/15m unconditionally), got %v", tf, cfg.Scanner.Timeframes)
		}
	}
	if cfg.Scanner.UniverseSize <= 0 {
		addf("scanner.universeSize must be > 0, got %d", cfg.Scanner.UniverseSize)
	}
	if cfg.Scanner.CandidateLimit <= 0 {
		addf("scanner.candidateLimit must be > 0, got %d", cfg.Scanner.CandidateLimit)
	}
	if cfg.Scanner.DeepAnalysisLimit <= 0 {
		addf("scanner.deepAnalysisLimit must be > 0, got %d", cfg.Scanner.DeepAnalysisLimit)
	}
	if cfg.Scanner.TotalTimeoutSeconds <= 0 {
		addf("scanner.totalTimeoutSeconds must be > 0, got %d", cfg.Scanner.TotalTimeoutSeconds)
	}
	if cfg.Scanner.ExchangeRequestTimeoutSeconds <= 0 {
		addf("scanner.exchangeRequestTimeoutSeconds must be > 0, got %d", cfg.Scanner.ExchangeRequestTimeoutSeconds)
	}
	if cfg.Scanner.SymbolAnalysisTimeoutSeconds <= 0 {
		addf("scanner.symbolAnalysisTimeoutSeconds must be > 0, got %d", cfg.Scanner.SymbolAnalysisTimeoutSeconds)
	}

	// --- alert policy ---------------------------------------------------------
	if cfg.AlertPolicy.Enabled {
		switch types.Severity(strings.ToUpper(cfg.AlertPolicy.MinSeverity)) {
		case types.SeverityLow, types.SeverityMedium, types.SeverityHigh:
		default:
			addf("alertPolicy.minSeverity %q is not one of LOW|MEDIUM|HIGH", cfg.AlertPolicy.MinSeverity)
		}
		if len(cfg.AlertPolicy.AllowedEventTypes) == 0 {
			addf("alertPolicy.allowedEventTypes must not be empty while alertPolicy.enabled=true (an empty list would silently allow every event type)")
		}
		if cfg.ResonanceScorer.Enabled && !containsString(cfg.AlertPolicy.AllowedEventTypes, "resonance") {
			addf("resonanceScorer.enabled=true but alertPolicy.allowedEventTypes lacks \"resonance\": resonance alerts would never be delivered")
		}
		if cfg.AlertPolicy.LiquidityWeight.Enabled {
			if w := cfg.AlertPolicy.LiquidityWeight.MinWeight; w < 0 || w > 1 {
				addf("alertPolicy.liquidityWeight.minWeight must be in [0,1], got %v", w)
			}
		}
	}

	// --- per-symbol detectors -------------------------------------------------
	if cfg.PriceVelocity.Enabled {
		if len(cfg.PriceVelocity.Windows) == 0 {
			addf("priceVelocity.windows must not be empty while priceVelocity.enabled=true")
		}
		for i, w := range cfg.PriceVelocity.Windows {
			if w.Seconds <= 0 || w.Threshold <= 0 {
				addf("priceVelocity.windows[%d] must have positive seconds and threshold, got seconds=%d threshold=%v", i, w.Seconds, w.Threshold)
			}
		}
		if cfg.PriceVelocity.CooldownSeconds < 0 {
			addf("priceVelocity.cooldownSeconds must be >= 0, got %d", cfg.PriceVelocity.CooldownSeconds)
		}
	}
	if cfg.VolumeSpike.Enabled {
		if cfg.VolumeSpike.Multiplier <= 1 {
			addf("volumeSpike.multiplier must be > 1, got %v", cfg.VolumeSpike.Multiplier)
		}
		if cfg.VolumeSpike.WindowMinutes <= 0 {
			addf("volumeSpike.windowMinutes must be > 0, got %d", cfg.VolumeSpike.WindowMinutes)
		}
	}
	if cfg.FuturesMetrics.Enabled && cfg.FuturesMetrics.PollIntervalSeconds <= 0 {
		addf("futuresMetrics.pollIntervalSeconds must be > 0, got %d", cfg.FuturesMetrics.PollIntervalSeconds)
	}
	if cfg.LongShortRatio.Enabled {
		if cfg.LongShortRatio.PollIntervalSeconds <= 0 {
			addf("longShortRatio.pollIntervalSeconds must be > 0, got %d", cfg.LongShortRatio.PollIntervalSeconds)
		}
		if cfg.LongShortRatio.ZscoreWindow <= 1 {
			addf("longShortRatio.zscoreWindow must be > 1, got %d", cfg.LongShortRatio.ZscoreWindow)
		}
	}
	if cfg.Liquidation.Enabled {
		if cfg.Liquidation.PollIntervalSeconds <= 0 {
			addf("liquidation.pollIntervalSeconds must be > 0, got %d", cfg.Liquidation.PollIntervalSeconds)
		}
		if cfg.Liquidation.ZscoreWindow <= 1 {
			addf("liquidation.zscoreWindow must be > 1, got %d", cfg.Liquidation.ZscoreWindow)
		}
	}
	if cfg.ResonanceScorer.Enabled {
		if cfg.ResonanceScorer.WindowSeconds <= 0 {
			addf("resonanceScorer.windowSeconds must be > 0, got %d", cfg.ResonanceScorer.WindowSeconds)
		}
		if cfg.ResonanceScorer.MinDimensions < 1 {
			addf("resonanceScorer.minDimensions must be >= 1, got %d", cfg.ResonanceScorer.MinDimensions)
		}
	}

	// --- MarketPulse ----------------------------------------------------------
	if cfg.MarketPulse.Enabled {
		mp := cfg.MarketPulse
		if mp.MinFreshRatio <= 0 || mp.MinFreshRatio > 1 {
			addf("marketPulse.minFreshRatio must be in (0,1], got %v", mp.MinFreshRatio)
		}
		if mp.Volatility.Enabled {
			if a := mp.Volatility.EWMAAlpha; a <= 0 || a > 1 {
				addf("marketPulse.volatility.ewmaAlpha must be in (0,1], got %v", a)
			}
		}
		if mp.SnapshotIntervalSeconds <= 0 {
			addf("marketPulse.snapshotIntervalSeconds must be > 0, got %d", mp.SnapshotIntervalSeconds)
		}
		if mp.MinValidSymbols <= 0 {
			addf("marketPulse.minValidSymbols must be > 0, got %d", mp.MinValidSymbols)
		}
		if mp.DataHealthAlertSeconds < 0 {
			addf("marketPulse.dataHealthAlertSeconds must be >= 0 (0 disables the outage alert), got %d", mp.DataHealthAlertSeconds)
		}
		if mp.MaxAlertsPerDay < 0 {
			addf("marketPulse.maxAlertsPerDay must be >= 0 (0 disables the budget), got %d", mp.MaxAlertsPerDay)
		}
		// The detector computes fixed 60s/180s/300s returns; retention must
		// cover the longest window or trend/outcome sampling silently starves.
		if mp.HistoryRetentionSeconds > 0 && mp.HistoryRetentionSeconds < 300 {
			addf("marketPulse.historyRetentionSeconds must be >= 300 (fixed 300s trend window), got %d", mp.HistoryRetentionSeconds)
		}
		checkConfirm := func(name string, samples, window int) {
			if samples <= 0 || window <= 0 {
				addf("marketPulse.%s confirmationSamples/confirmationWindowSamples must be > 0, got %d/%d", name, samples, window)
			} else if samples > window {
				addf("marketPulse.%s.confirmationSamples (%d) must not exceed confirmationWindowSamples (%d): the condition could never confirm", name, samples, window)
			}
		}
		checkConfirm("impulse", mp.Impulse.ConfirmationSamples, mp.Impulse.ConfirmationWindowSamples)
		checkConfirm("trend", mp.Trend.ConfirmationSamples, mp.Trend.ConfirmationWindowSamples)
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid config (%d problems):\n  - %s", len(errs), strings.Join(errs, "\n  - "))
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
