# Reduce Noisy Trading Alerts

## Problem

Kairos sends too many webhook events to Hermes on `ccs`. The user only wants concise KISS-style trading opportunity alerts, not every raw price or volume anomaly.

## Findings

- `ccs` has no Hermes cron jobs; noise comes from Kairos WebSocket anomaly events posted to the `kairos-signals` webhook.
- Remote config watches OKX Top 50 with price velocity and volume spike detectors enabled.
- Current gateway logs show hundreds of webhook inbound messages, while Kairos MCP stderr shows thousands of delivered anomaly events.
- Several symbols repeat many times in current logs, especially `price_velocity` and `volume_spike` events.
- `DataManager` passes section configs into detectors, while detectors expect nested top-level config keys, so detector tuning can be ignored.
- Hermes has `kairos-harness` installed, but webhook runs still try nonexistent MCP prompts/resources, so source-side filtering is needed.

## Requirements

- Detector configs must honor runtime YAML values when constructed by `DataManager`.
- Kairos must support a source-side alert policy before webhook delivery.
- Alert policy must be configurable by event type, minimum severity, minimum price change, and minimum volume ratio.
- Existing behavior should remain available through permissive policy settings.
- `ccs` should be moved to a conservative KISS config: fewer symbols, no pure volume-spike alerts, larger price-move threshold, longer same-symbol cooldown.

## Acceptance Criteria

- Tests cover detector flat-config handling and DataManager alert-policy drops.
- Related test subset passes locally.
- `ccs` config is updated without exposing secrets.
- Hermes gateway is restarted and Kairos starts with the new config.
