package types

// AnomalyEvent is emitted when an anomaly is detected by a real-time detector.
type AnomalyEvent struct {
	Symbol    string         `json:"symbol" yaml:"symbol"`
	EventType string         `json:"event_type" yaml:"event_type"`
	Severity  Severity       `json:"severity" yaml:"severity"`
	Data      map[string]any `json:"data" yaml:"data"`
	// Timestamp is Unix seconds (fractional). The pipeline backfills it when a
	// detector leaves it zero.
	Timestamp float64 `json:"timestamp" yaml:"timestamp"`
	// Exchange is the originating exchange id (e.g. "okx"). Filled by the
	// pipeline when events enter the aggregator; detectors may leave it empty.
	Exchange string `json:"exchange,omitempty" yaml:"exchange,omitempty"`
	// EventID is a deterministic id derived from exchange/symbol/type/timestamp
	// and payload, assigned by the pipeline for tracing and delivery audit.
	EventID string `json:"event_id,omitempty" yaml:"event_id,omitempty"`
}
