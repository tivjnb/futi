package core

import (
	"fmt"
	"os"
	"time"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/modulecache"
)

// CacheStatus is the state of the module cache.
type CacheStatus struct {
	Policy      string    // (*modulecache.Cache).PolicyName()
	LastRefresh time.Time // zero = never
	Modules     int
	Path        string // "" = in-memory, no persistent cache
}

// CacheStatusOf loads the cache from disk and reports its state; path is the
// cache file path ("" for an in-memory cache).
func CacheStatusOf(c *modulecache.Cache, path string) CacheStatus {
	c.Load() // populate from disk for accurate module count
	return CacheStatus{
		Policy:      c.PolicyName(),
		LastRefresh: c.LastRefresh(),
		Modules:     c.ModuleCount(),
		Path:        path,
	}
}

// RefreshCache loads the cache from disk and refreshes it from global. The
// error carries the "cache refresh: " prefix; with a *discovery.PartialError
// in the chain the result is valid and the error is not classified.
func RefreshCache(c *modulecache.Cache, global discovery.Source) (modulecache.RefreshResult, error) {
	c.Load() // load from disk without Refresh
	result, err := c.Refresh(global)
	if err != nil {
		return result, classifyUnlessPartial(fmt.Errorf("cache refresh: %w", err))
	}
	return result, nil
}

// ClearCache removes the persistent cache file. An empty path (in-memory
// cache) and a missing file are not errors.
func ClearCache(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return classify(fmt.Errorf("cache clear: remove %s: %w", path, err))
	}
	return nil
}

// CacheStatus reports the runtime cache state, see CacheStatusOf.
// Every call re-reads the cache from disk (Load), which matches the CLI
// (one process runs one command). With in-memory storage Load resets
// LastRefresh to zero; revisit this for long-lived transports.
func (r *Runtime) CacheStatus() CacheStatus { return CacheStatusOf(r.Cache(), r.CachePath()) }

// CacheRefresh refreshes the cache from the global source (not the merged
// one with local layers), see RefreshCache.
// Like CacheStatus, every call re-reads the cache from disk (Load); see the
// note there about in-memory storage and long-lived transports.
func (r *Runtime) CacheRefresh() (modulecache.RefreshResult, error) {
	return RefreshCache(r.Cache(), r.GlobalSource())
}

// CacheClear removes the runtime's persistent cache file, see ClearCache.
func (r *Runtime) CacheClear() error { return ClearCache(r.CachePath()) }
