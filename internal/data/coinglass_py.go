package data

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	envCoinGlassDecrypt = "KAIROS_COINGLASS_DECRYPT"
	envCoinGlassPython  = "KAIROS_COINGLASS_PYTHON"
	envCoinGlassUsePy   = "KAIROS_COINGLASS_USE_PYTHON"
)

// fetchCoinGlassViaPython calls scripts/coinglass_fetch.py (ArchdevilForge coinglass-decrypt).
func fetchCoinGlassViaPython(path string, params map[string]string, timeout time.Duration) (any, error) {
	if !coinGlassUsePython() {
		return nil, fmt.Errorf("coinglass python disabled")
	}
	decryptRoot := resolveCoinGlassDecryptRoot()
	if decryptRoot == "" {
		return nil, fmt.Errorf("coinglass-decrypt path not found")
	}
	script := resolveCoinGlassFetchScript()
	if _, err := os.Stat(script); err != nil {
		return nil, fmt.Errorf("coinglass_fetch.py missing: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	python := resolveCoinGlassPython(decryptRoot)
	paramStr := encodeCoinGlassParams(params)
	cmd := exec.CommandContext(ctx, python, script, "--path", path, "--params", paramStr, "--timeout", fmt.Sprintf("%d", int(timeout.Seconds())))
	cmd.Env = append(os.Environ(), envCoinGlassDecrypt+"="+decryptRoot)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("coinglass python: %s", msg)
	}

	var payload any
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("coinglass python json: %w", err)
	}
	return unwrapCoinGlassPayload(payload), nil
}

func coinGlassUsePython() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envCoinGlassUsePy))) {
	case "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	default:
		return resolveCoinGlassDecryptRoot() != ""
	}
}

func resolveCoinGlassDecryptRoot() string {
	if p := strings.TrimSpace(os.Getenv(envCoinGlassDecrypt)); p != "" {
		if st, err := os.Stat(filepath.Join(p, "decrypt.py")); err == nil && !st.IsDir() {
			return p
		}
		return ""
	}
	for _, candidate := range coinGlassDecryptCandidates() {
		if st, err := os.Stat(filepath.Join(candidate, "decrypt.py")); err == nil && !st.IsDir() {
			return candidate
		}
	}
	return ""
}

func coinGlassDecryptCandidates() []string {
	root := findKairosRoot()
	sibling := filepath.Clean(filepath.Join(root, "..", "coinglass-decrypt"))
	return []string{sibling}
}

func findKairosRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return wd
}

func resolveCoinGlassFetchScript() string {
	if p := strings.TrimSpace(os.Getenv("KAIROS_COINGLASS_FETCH_SCRIPT")); p != "" {
		return p
	}
	return filepath.Join(findKairosRoot(), "scripts", "coinglass_fetch.py")
}

func resolveCoinGlassPython(decryptRoot string) string {
	if p := strings.TrimSpace(os.Getenv(envCoinGlassPython)); p != "" {
		return p
	}
	venvPy := filepath.Join(decryptRoot, ".venv", "bin", "python")
	if st, err := os.Stat(venvPy); err == nil && !st.IsDir() {
		return venvPy
	}
	return "python3"
}

func encodeCoinGlassParams(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	parts := make([]string, 0, len(params))
	for k, v := range params {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}

func unwrapCoinGlassPayload(payload any) any {
	m, ok := payload.(map[string]any)
	if !ok {
		return payload
	}
	if d, has := m["data"]; has && len(m) <= 4 {
		return d
	}
	return payload
}
