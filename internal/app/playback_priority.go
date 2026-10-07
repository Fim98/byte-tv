package app

import "context"

func backgroundPlayback(ctx context.Context) bool {
	switch value := ctx.Value(playbackPrefetchKey{}).(type) {
	case *playbackPrefetch:
		return !value.foreground.Load()
	case bool:
		return value
	default:
		return false
	}
}

func (cache *playbackPrefetch) promote() bool {
	cache.mu.Lock()
	if cache.preempted {
		cache.mu.Unlock()
		return false
	}
	cache.foreground.Store(true)
	cache.mu.Unlock()
	if cache.native != nil {
		cache.native.mu.Lock()
		cache.native.background = false
		cache.native.prefillLocked(cache.native.wanted)
		cache.native.mu.Unlock()
	}
	return true
}

func (cache *playbackPrefetch) cancelBackground() {
	cache.mu.Lock()
	interrupt := !cache.foreground.Load() && !cache.preempted
	if interrupt {
		cache.preempted = true
	}
	cache.mu.Unlock()
	if interrupt {
		cache.cancel()
	}
}
