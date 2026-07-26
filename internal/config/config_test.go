package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestLoadString_Defaults(t *testing.T) {
	cfg, err := LoadString("")
	if err != nil {
		t.Fatalf("LoadString empty: %v", err)
	}
	if cfg.Exchange != "okx" {
		t.Fatalf("exchange: got %q want okx", cfg.Exchange)
	}
	if cfg.DataManager.TopSymbols != 30 {
		t.Fatalf("topSymbols: got %d want 30", cfg.DataManager.TopSymbols)
	}
	if cfg.Scanner.UniverseSize != 30 {
		t.Fatalf("universeSize: got %d want 30", cfg.Scanner.UniverseSize)
	}
	if cfg.Exchanges.Primary != "okx" {
		t.Fatalf("primary: got %q", cfg.Exchanges.Primary)
	}
}

func TestLoadString_Override(t *testing.T) {
	yaml := `
exchange: binance
dataManager:
  topSymbols: 200
scanner:
  universeSize: 12
`
	cfg, err := LoadString(yaml)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Exchange != "binance" {
		t.Fatalf("exchange: got %q", cfg.Exchange)
	}
	if cfg.DataManager.TopSymbols != 200 {
		t.Fatalf("topSymbols: got %d", cfg.DataManager.TopSymbols)
	}
	if cfg.Scanner.UniverseSize != 12 {
		t.Fatalf("universeSize: got %d", cfg.Scanner.UniverseSize)
	}
	// Legacy alias must propagate to both authorities.
	if cfg.Exchanges.Primary != "binance" {
		t.Fatalf("primary should follow legacy exchange alias, got %q", cfg.Exchanges.Primary)
	}
	if len(cfg.DataManager.Exchanges) != 1 || cfg.DataManager.Exchanges[0] != "binance" {
		t.Fatalf("dataManager.exchanges should follow legacy alias, got %v", cfg.DataManager.Exchanges)
	}
}

func TestLoad_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("exchange: bybit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Exchange != "bybit" || cfg.Exchanges.Primary != "bybit" {
		t.Fatalf("exchange: got %q primary %q", cfg.Exchange, cfg.Exchanges.Primary)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	cfg, err := LoadString("exchange: okx")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELEGRAM_BOT_TOKEN", "tok")
	t.Setenv("TELEGRAM_CHAT_ID", "-100")
	t.Setenv("DINGTALK_WEBHOOK_URL", "https://oapi.dingtalk.com/robot/send?access_token=x")
	t.Setenv("DINGTALK_SECRET", "SECxyz")
	LoadEnvOverrides(cfg)
	if cfg.Telegram.BotToken != "tok" {
		t.Fatalf("token: %q", cfg.Telegram.BotToken)
	}
	if cfg.Telegram.ChatID != "-100" {
		t.Fatalf("chat: %q", cfg.Telegram.ChatID)
	}
	if cfg.DingTalk.WebhookURL == "" || cfg.DingTalk.Secret != "SECxyz" {
		t.Fatalf("dingtalk env: %+v", cfg.DingTalk)
	}
}

func TestSecretsNeverSerialized(t *testing.T) {
	cfg, err := LoadString("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Telegram.BotToken = "super-secret-token"
	cfg.Telegram.ChatID = "-1001234"
	cfg.DingTalk.WebhookURL = "https://oapi.dingtalk.com/robot/send?access_token=tok"
	cfg.DingTalk.Secret = "SECsecret"

	jsonOut, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	yamlOut, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"super-secret-token", "-1001234", "access_token=tok", "SECsecret"} {
		if strings.Contains(string(jsonOut), leak) {
			t.Fatalf("JSON serialization leaks secret %q", leak)
		}
		if strings.Contains(string(yamlOut), leak) {
			t.Fatalf("YAML serialization leaks secret %q", leak)
		}
	}
}

func TestValidate_RejectsBrokenConfigs(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "primary not in realtime set",
			yaml: "exchanges:\n  primary: binance\ndataManager:\n  exchanges: [okx]\n",
			want: "must be included in dataManager.exchanges",
		},
		{
			name: "unknown exchange",
			yaml: "exchange: kraken\n",
			want: "not a supported exchange",
		},
		{
			name: "missing core timeframe",
			yaml: "scanner:\n  timeframes: [\"1d\", \"4h\"]\n",
			want: "scanner.timeframes must include",
		},
		{
			name: "negative deep analysis limit",
			yaml: "scanner:\n  deepAnalysisLimit: -1\n",
			want: "deepAnalysisLimit",
		},
		{
			name: "bad severity",
			yaml: "alertPolicy:\n  enabled: true\n  minSeverity: CRITICAL\n",
			want: "minSeverity",
		},
		{
			name: "empty allow list while enabled",
			yaml: "alertPolicy:\n  enabled: true\n  allowedEventTypes: []\n",
			want: "allowedEventTypes must not be empty",
		},
		{
			name: "resonance enabled but not allowed",
			yaml: "resonanceScorer:\n  enabled: true\nalertPolicy:\n  enabled: true\n  allowedEventTypes: [\"price_velocity\"]\n",
			want: "lacks \"resonance\"",
		},
		{
			name: "market pulse ratio out of range",
			yaml: "marketPulse:\n  enabled: true\n  minFreshRatio: 1.5\n",
			want: "minFreshRatio",
		},
		{
			name: "confirmation samples exceed window",
			yaml: "marketPulse:\n  enabled: true\n  impulse:\n    confirmationSamples: 5\n    confirmationWindowSamples: 4\n",
			want: "confirmationSamples",
		},
		{
			name: "retention below fixed trend window",
			yaml: "marketPulse:\n  enabled: true\n  historyRetentionSeconds: 120\n",
			want: "historyRetentionSeconds",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadString(tc.yaml)
			if err == nil {
				t.Fatalf("expected validation error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestValidate_AggregatesAllProblems(t *testing.T) {
	_, err := LoadString("exchange: kraken\nscanner:\n  universeSize: -1\n  timeframes: [\"1d\"]\n")
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"kraken", "universeSize", "timeframes"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("aggregated error missing %q: %v", want, err)
		}
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/kairos/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadString_DefaultsLiquidityWeight(t *testing.T) {
	cfg, err := LoadString("")
	if err != nil {
		t.Fatal(err)
	}
	lw := cfg.AlertPolicy.LiquidityWeight
	if !lw.Enabled || lw.MinWeight != 0.5 {
		t.Fatalf("defaults: enabled=%v minWeight=%v", lw.Enabled, lw.MinWeight)
	}
	if len(lw.MajorSymbols) != 2 {
		t.Fatalf("majorSymbols: %v", lw.MajorSymbols)
	}
}

func TestLoadString_DefaultsMarketPulse(t *testing.T) {
	cfg, err := LoadString("")
	if err != nil {
		t.Fatal(err)
	}
	mp := cfg.MarketPulse
	if mp.Enabled {
		t.Fatal("marketPulse.enabled default should be false")
	}
	if !mp.ShadowMode {
		t.Fatal("shadowMode default should be true")
	}
	if mp.MinValidSymbols != 15 {
		t.Fatalf("minValidSymbols=%d", mp.MinValidSymbols)
	}
	if mp.Impulse.MinBreadth != 0.75 {
		t.Fatalf("impulse.minBreadth=%v", mp.Impulse.MinBreadth)
	}
	if mp.Impulse.MinMedianReturnPct != 0.35 {
		t.Fatalf("impulse.minMedianReturnPct=%v", mp.Impulse.MinMedianReturnPct)
	}
	if mp.DataHealthAlertSeconds != 900 {
		t.Fatalf("dataHealthAlertSeconds=%d", mp.DataHealthAlertSeconds)
	}
	if mp.MaxAlertsPerDay != 6 {
		t.Fatalf("maxAlertsPerDay=%d", mp.MaxAlertsPerDay)
	}
	if mp.MinFreshRatio != 0.60 {
		t.Fatalf("minFreshRatio=%v", mp.MinFreshRatio)
	}
	if mp.GateIndividualAlertsWhenQuiet {
		t.Fatal("gate should be off by default in Phase 1")
	}
}

func TestLoadString_MarketPulseOverride(t *testing.T) {
	yaml := `
marketPulse:
  enabled: true
  shadowMode: true
  minValidSymbols: 12
  impulse:
    minBreadth: 0.7
`
	cfg, err := LoadString(yaml)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MarketPulse.Enabled || !cfg.MarketPulse.ShadowMode {
		t.Fatalf("enabled/shadow: %+v", cfg.MarketPulse)
	}
	if cfg.MarketPulse.MinValidSymbols != 12 {
		t.Fatalf("minValidSymbols=%d", cfg.MarketPulse.MinValidSymbols)
	}
	if cfg.MarketPulse.Impulse.MinBreadth != 0.7 {
		t.Fatalf("minBreadth=%v", cfg.MarketPulse.Impulse.MinBreadth)
	}
}

func TestLoad_ExampleConfigAlignsWithTypes(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config", "config.yaml.example"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scoring.CandidateWeights["rsihotness"] != 1.0 {
		t.Fatalf("rsiHotness: %v", cfg.Scoring.CandidateWeights)
	}
	if cfg.Scoring.CandidateWeights["btcrelativestrength"] != 1.5 {
		t.Fatalf("btcRelativeStrength: %v", cfg.Scoring.CandidateWeights)
	}
	if cfg.LongShortRatio.Enabled || cfg.Liquidation.Enabled || cfg.ResonanceScorer.Enabled {
		t.Fatalf("detector sections: ls=%v liq=%v res=%v",
			cfg.LongShortRatio.Enabled, cfg.Liquidation.Enabled, cfg.ResonanceScorer.Enabled)
	}
	wantTypes := map[string]bool{
		"price_velocity": true,
		"resonance":      true,
		"market_impulse": true,
		"market_trend":   true,
		"market_stress":  true,
		"market_decay":   true,
	}
	if len(cfg.AlertPolicy.AllowedEventTypes) != len(wantTypes) {
		t.Fatalf("allowedEventTypes: %v", cfg.AlertPolicy.AllowedEventTypes)
	}
	for _, tpe := range cfg.AlertPolicy.AllowedEventTypes {
		if !wantTypes[tpe] {
			t.Fatalf("unexpected allowedEventType %q in %v", tpe, cfg.AlertPolicy.AllowedEventTypes)
		}
	}
	if !cfg.AlertPolicy.LiquidityWeight.Enabled || cfg.AlertPolicy.LiquidityWeight.MinWeight != 0.5 {
		t.Fatalf("liquidityWeight: enabled=%v minWeight=%v",
			cfg.AlertPolicy.LiquidityWeight.Enabled, cfg.AlertPolicy.LiquidityWeight.MinWeight)
	}
	if len(cfg.AlertPolicy.LiquidityWeight.MajorSymbols) < 2 {
		t.Fatalf("majorSymbols: %v", cfg.AlertPolicy.LiquidityWeight.MajorSymbols)
	}
	// Example keeps marketPulse disabled (Phase 1 opt-in).
	if cfg.MarketPulse.Enabled {
		t.Fatal("example marketPulse.enabled should be false")
	}
	if !cfg.MarketPulse.ShadowMode {
		t.Fatal("example marketPulse.shadowMode should be true")
	}
}
