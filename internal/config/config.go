package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/spf13/viper"
)

// Load reads config from a YAML file, applies environment overrides, and
// returns the parsed Config.
func Load(path string) (*types.Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	// Set defaults matching the Python KairosArchitectureConfig defaults
	setDefaults(v)

	var cfg types.Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	LoadEnvOverrides(&cfg)
	return &cfg, nil
}

// LoadString reads config from a raw YAML string (useful for tests).
func LoadString(yamlContent string) (*types.Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")

	if err := v.ReadConfig(strings.NewReader(yamlContent)); err != nil {
		return nil, fmt.Errorf("read yaml string: %w", err)
	}

	setDefaults(v)

	var cfg types.Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	LoadEnvOverrides(&cfg)
	return &cfg, nil
}

// LoadEnvOverrides applies environment variable overrides to the config.
// Only sets a field when the env var is non-empty.
func LoadEnvOverrides(cfg *types.Config) {
	if token := os.Getenv("TELEGRAM_BOT_TOKEN"); token != "" {
		cfg.Telegram.BotToken = token
	}
	if chatID := os.Getenv("TELEGRAM_CHAT_ID"); chatID != "" {
		cfg.Telegram.ChatID = chatID
	}
	if minState := os.Getenv("KAIROS_ALERT_MIN_STATE"); minState != "" {
		cfg.AlertMinState = minState
	}
	if limitStr := os.Getenv("KAIROS_ALERT_LIMIT"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			cfg.AlertLimit = limit
		}
	}
}

// setDefaults populates viper defaults matching the Python _DEFAULT_CONFIG.
func setDefaults(v *viper.Viper) {
	v.SetDefault("exchange", "okx")
	v.SetDefault("defaultTimeframe", "1d")
	v.SetDefault("notificationTimezone", "Asia/Shanghai")

	v.SetDefault("telegram", map[string]any{
		"enabled":   true,
		"parseMode": "HTML",
	})

	v.SetDefault("dataManager", map[string]any{
		"exchanges":             []string{"okx", "binance", "bybit"},
		"topSymbols":            30,
		"refreshIntervalHours":  4,
		"dedupWindowSeconds":    5,
		"symbolCooldownMinutes": 30,
	})

	v.SetDefault("alertPolicy", map[string]any{
		"enabled":                true,
		"allowedEventTypes":      []string{"price_velocity", "volume_spike", "open_interest_change", "funding_rate_anomaly", "long_short_ratio", "liquidation", "resonance"},
		"minSeverity":            "MEDIUM",
		"minPriceChangePct":      1.2,
		"minVolumeRatio":         6.0,
		"minOpenInterestChangePct": 5.0,
		"minFundingRateAbs":      0.0005,
		"minFundingRateChangeAbs": 0.0003,
	})

	v.SetDefault("priceVelocity", map[string]any{
		"enabled": true,
		"windows": []map[string]any{
			{"seconds": 30, "threshold": 0.5},
			{"seconds": 60, "threshold": 0.8},
			{"seconds": 120, "threshold": 1.2},
		},
		"cooldownSeconds": 60,
	})

	v.SetDefault("volumeSpike", map[string]any{
		"enabled":           true,
		"multiplier":        3.0,
		"windowMinutes":     10,
		"minHistorySeconds": 600,
		"minNotifyInterval": "2m",
	})

	v.SetDefault("futuresMetrics", map[string]any{
		"enabled":               true,
		"pollIntervalSeconds":   300,
		"fetchFundingPerSymbol": true,
		"openInterest": map[string]any{
			"enabled":          true,
			"minChangePct":     5.0,
			"minNotifyInterval": "30m",
		},
		"fundingRate": map[string]any{
			"enabled":            true,
			"absRateThreshold":   0.0005,
			"minChangeAbs":       0.0003,
			"minNotifyInterval":  "30m",
		},
	})

	v.SetDefault("longShortRatio", map[string]any{
		"enabled":              true,
		"pollIntervalSeconds":  300,
		"absThreshold":         80.0,
		"zscoreThreshold":      2.5,
		"zscoreWindow":         48,
		"velocityThresholdPct": 15.0,
		"minNotifyInterval":    "30m",
	})

	v.SetDefault("liquidation", map[string]any{
		"enabled":              true,
		"pollIntervalSeconds":  300,
		"absThresholdUsd":      1_000_000,
		"zscoreThreshold":      2.5,
		"zscoreWindow":         48,
		"imbalanceThreshold":   0.80,
		"minNotifyInterval":    "30m",
	})

	v.SetDefault("resonanceScorer", map[string]any{
		"enabled":          true,
		"windowSeconds":    300,
		"minDimensions":    2,
		"minScore":         55,
		"cooldownSeconds":  600,
	})

	v.SetDefault("scanner", map[string]any{
		"intervalSeconds":              300,
		"universeSize":                 30,
		"candidateLimit":               20,
		"deepAnalysisLimit":            10,
		"totalTimeoutSeconds":          75,
		"exchangeRequestTimeoutSeconds": 8,
		"symbolAnalysisTimeoutSeconds":  12,
		"timeframes":                   []string{"1d", "4h", "15m"},
		"generateChartsByDefault":      false,
	})

	v.SetDefault("exchanges", map[string]any{
		"primary":        "okx",
		"backups":        []string{"binance", "bybit"},
		"rateLimit":      true,
		"canonicalQuote": "USDT",
		"settle":       "USDT",
	})

	v.SetDefault("scoring", map[string]any{
		"candidateWeights": map[string]any{
			"quoteVolume":         4.0,
			"priceVelocity":       2.0,
			"openInterest":        1.0,
			"funding":             1.0,
			"relativeStrength":    2.0,
			"btcRelativeStrength": 1.5,
			"rsiHotness":          1.0,
		},
		"setupWeights": map[string]any{
			"dailyTrend":         1.5,
			"structure":          2.0,
			"entryTrigger":       2.0,
			"btcResonance":       1.0,
			"marketCycle":        1.0,
			"volumeConfirmation": 1.0,
			"riskReward":         1.5,
		},
		"cycleThresholds": map[string]any{
			"spring": 5.5,
			"summer": 5.5,
			"autumn": 6.5,
			"winter": 7.5,
		},
		"minimumLiquidityQuoteVolume": 30_000_000.0,
		"minimumRiskReward":           1.8,
		"strictRiskReward":            2.2,
		"shortThresholdPremium":       0.5,
		"cycleDetector": map[string]any{
			"springBtcChangeMin":      10.0,
			"summerBtcChangeMin":      30.0,
			"autumnBtcChangeMax":      50.0,
			"winterBtcChangeMax":      -10.0,
			"highVolatilityThreshold": 5.0,
			"lowVolatilityThreshold":  2.0,
			"highFundingThreshold":    0.05,
			"lowFundingThreshold":     -0.01,
		},
	})

	v.SetDefault("risk", map[string]any{
		"maxPositionPct": map[string]any{
			"major":   33.0,
			"altcoin": 33.0,
		},
		"maxLeverage": map[string]any{
			"major":   10.0,
			"altcoin": 5.0,
		},
		"weakCyclePositionMultiplier":     0.5,
		"shortPositionMultiplier":         0.75,
		"inverseCyclePositionMultiplier":  0.5,
	})

	v.SetDefault("storage", map[string]any{
		"databasePath":            "~/.local/share/kairos/kairos.db",
		"retentionDays":           90,
		"jsonlExport":             false,
		"jsonlPath":               "",
		"watchHintRetentionHours": 24.0,
		"watchHintScoreBoost":     0.5,
	})

	v.SetDefault("charts", map[string]any{
		"defaultChartCount":           1,
		"outputPath":                  "~/.local/share/kairos/charts",
		"cleanupDays":                 7,
		"multiTimeframeScoreThreshold": 8.0,
	})
}
