package config

import (
	"os"
	"path/filepath"
	"testing"
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
	// preserved default
	if cfg.NotificationTimezone != "Asia/Shanghai" {
		t.Fatalf("timezone: got %q", cfg.NotificationTimezone)
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
	if cfg.Exchange != "bybit" {
		t.Fatalf("exchange: got %q", cfg.Exchange)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	cfg, err := LoadString("exchange: okx")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELEGRAM_BOT_TOKEN", "tok")
	t.Setenv("TELEGRAM_CHAT_ID", "-100")
	t.Setenv("KAIROS_ALERT_MIN_STATE", "watch")
	t.Setenv("KAIROS_ALERT_LIMIT", "3")
	LoadEnvOverrides(cfg)
	if cfg.Telegram.BotToken != "tok" {
		t.Fatalf("token: %q", cfg.Telegram.BotToken)
	}
	if cfg.Telegram.ChatID != "-100" {
		t.Fatalf("chat: %q", cfg.Telegram.ChatID)
	}
	if cfg.AlertMinState != "watch" {
		t.Fatalf("min state: %q", cfg.AlertMinState)
	}
	if cfg.AlertLimit != 3 {
		t.Fatalf("limit: %d", cfg.AlertLimit)
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
	if mp.Impulse.MinBreadth != 0.65 {
		t.Fatalf("impulse.minBreadth=%v", mp.Impulse.MinBreadth)
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
