// Package storage persists cross-runtime hints (watch → scanner).
package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

type watchHint struct {
	Symbol    string    `json:"symbol"`
	EventType string    `json:"event_type"`
	Exchange  string    `json:"exchange"`
	At        time.Time `json:"at"`
}

// HintStore append-only JSONL store for recent watch-path anomalies.
type HintStore struct {
	path      string
	retention time.Duration
	boost     float64
	mu        sync.Mutex
}

// NewHintStore opens or creates the hint log from storage config.
func NewHintStore(cfg types.StorageConfig) (*HintStore, error) {
	path := cfg.WatchHintsPath
	if path == "" {
		dir := filepath.Dir(expandPath(cfg.DatabasePath))
		path = filepath.Join(dir, "watch-hints.jsonl")
	} else {
		path = expandPath(path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("hint store mkdir: %w", err)
	}
	retention := time.Duration(cfg.WatchHintRetentionHours) * time.Hour
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	boost := cfg.WatchHintScoreBoost
	if boost <= 0 {
		boost = 0.5
	}
	return &HintStore{path: path, retention: retention, boost: boost}, nil
}

// Record appends a watch anomaly hint for scanner boosting.
func (h *HintStore) Record(symbol, eventType, exchange string) error {
	if h == nil || symbol == "" {
		return nil
	}
	rec := watchHint{
		Symbol:    symbol,
		EventType: eventType,
		Exchange:  exchange,
		At:        time.Now().UTC(),
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return appendJSONLLine(h.path, rec)
}

// ActiveBoosts returns per-symbol candidate_score boost for hints inside retention.
func (h *HintStore) ActiveBoosts() map[string]float64 {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	active := h.readActiveLocked()
	out := make(map[string]float64, len(active))
	for sym := range active {
		out[sym] = h.boost
	}
	return out
}

func (h *HintStore) readActiveLocked() map[string]watchHint {
	cutoff := time.Now().UTC().Add(-h.retention)
	latest := make(map[string]watchHint)

	_ = scanJSONLLines(h.path, 0, func(line string) {
		var rec watchHint
		if err := json.Unmarshal([]byte(line), &rec); err != nil || rec.Symbol == "" {
			return
		}
		if rec.At.Before(cutoff) {
			return
		}
		if prev, ok := latest[rec.Symbol]; !ok || rec.At.After(prev.At) {
			latest[rec.Symbol] = rec
		}
	})
	return latest
}

func appendJSONLLine(path string, v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(append(line, '\n'))
	return err
}

func scanJSONLLines(path string, maxTokenSize int, fn func(line string)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	if maxTokenSize > 0 {
		sc.Buffer(make([]byte, 0, 64*1024), maxTokenSize)
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fn(line)
	}
	return sc.Err()
}

func expandPath(p string) string {
	if p == "" {
		return p
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}
