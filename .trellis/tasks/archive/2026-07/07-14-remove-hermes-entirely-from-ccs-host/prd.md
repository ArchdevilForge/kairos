# PRD: Remove Hermes entirely from ccs

## Goal
Completely uninstall Hermes Agent (gateway, MCP, data, binaries) from host `ccs`. Kairos alert path must not depend on it.

## Scope
- Stop and disable user unit `hermes-gateway.service`
- Remove unit file and wants
- Delete `/usr/local/lib/hermes-agent`, `/usr/local/bin/hermes`, `/root/.hermes`, related state/cache if hermes-only
- Kill any remaining hermes processes
- Optional: remove dead `/root/kairos` if only Hermes MCP leftover

## Out of scope
- smartalpha, axonhub, kairos-go
- Local developer machine Hermes installs (unless present under kairos repo)

## Acceptance
- `pgrep -af hermes` empty
- No hermes unit enabled
- Paths above gone
- `kairos-go` still active
