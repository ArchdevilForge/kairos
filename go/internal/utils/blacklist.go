package utils

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Blacklist is a read-only set of blocked symbols loaded from
// ~/.config/kairos/blacklist.txt (one symbol per line).
type Blacklist struct {
	blocked map[string]struct{}
}

// NewBlacklist loads the blacklist file and returns a Blacklist.
func NewBlacklist() *Blacklist {
	b := &Blacklist{blocked: make(map[string]struct{})}
	home, err := os.UserHomeDir()
	if err != nil {
		return b
	}
	f, err := os.Open(filepath.Join(home, ".config", "kairos", "blacklist.txt"))
	if err != nil {
		return b // file doesn't exist → empty blacklist
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			b.blocked[strings.ToUpper(line)] = struct{}{}
		}
	}
	return b
}

// IsBlocked reports whether the given symbol is on the blacklist.
func (b *Blacklist) IsBlocked(symbol string) bool {
	_, ok := b.blocked[strings.ToUpper(symbol)]
	return ok
}
