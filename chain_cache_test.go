package main

import (
    "context"
    "testing"
    "time"
)

func TestStartChainCacheCleanup(t *testing.T) {
    ttl := 10 * time.Millisecond
    cs := &ChainState{cache: &cachedChain{combo: []*Proxy{{}}, lastUsed: time.Now().Add(-2 * ttl)}}
    userChains.Store(map[string]*ChainState{"u": cs})
    defer userChains.Store(map[string]*ChainState{})

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    startChainCacheCleanup(ctx, ttl)

    // Wait until cache is cleared or timeout
    for i := 0; i < 10; i++ {
        cs.cacheMu.RLock()
        cleared := cs.cache == nil
        cs.cacheMu.RUnlock()
        if cleared {
            return
        }
        time.Sleep(ttl)
    }
    t.Fatal("cache was not cleaned")
}

