package audit

import (
	"fmt"
	"sync"
	"time"
)

// PreparedCache is intentionally local-only. It never serializes prepared
// witnesses and it discards entries as soon as the active key, artifact set,
// network or expiry changes.
type PreparedCache struct {
	mu      sync.Mutex
	entries map[string]*Prepared
}

func NewPreparedCache() *PreparedCache { return &PreparedCache{entries: make(map[string]*Prepared)} }

func (c *PreparedCache) Put(key string, prepared *Prepared) error {
	if c == nil || key == "" || prepared == nil {
		return fmt.Errorf("prepared cache key and value are required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if old := c.entries[key]; old != nil && old != prepared {
		old.Clear()
	}
	c.entries[key] = prepared
	return nil
}

func (c *PreparedCache) Take(key string, snapshot Snapshot, now time.Time) (*Prepared, bool) {
	if c == nil || key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	prepared := c.entries[key]
	if prepared == nil {
		return nil, false
	}
	if !prepared.ValidFor(snapshot, now) {
		prepared.Clear()
		delete(c.entries, key)
		return nil, false
	}
	return prepared, true
}

func (c *PreparedCache) Invalidate(snapshot Snapshot) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, prepared := range c.entries {
		if !prepared.ValidFor(snapshot, time.Time{}) {
			prepared.Clear()
			delete(c.entries, key)
		}
	}
}

func (c *PreparedCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, prepared := range c.entries {
		prepared.Clear()
		delete(c.entries, key)
	}
}
