package types

// AnomalyEvent is emitted when an anomaly is detected by a real-time detector.
type AnomalyEvent struct {
	Symbol    string         `json:"symbol" yaml:"symbol"`
	EventType string         `json:"event_type" yaml:"event_type"`
	Severity  Severity       `json:"severity" yaml:"severity"`
	Data      map[string]any `json:"data" yaml:"data"`
	Timestamp float64        `json:"timestamp" yaml:"timestamp"`
}
