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
