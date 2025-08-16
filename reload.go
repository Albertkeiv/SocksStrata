package main

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

var (
	userChains atomic.Value
	chainsMu   sync.RWMutex
)

func startConfigReload(ctx context.Context, cfg *Config) {
	interval := cfg.General.ConfigReloadInterval
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			newCfg, err := loadConfig(*configPath)
			if err != nil {
				warnLog.Printf("config reload failed: %v", err)
				continue
			}
			initProxies(&newCfg)
			newChains, err := buildUserChains(newCfg.Chains)
			if err != nil {
				warnLog.Printf("config reload build chains: %v", err)
				continue
			}
			chainCacheMu.Lock()
			chainCache = make(map[string]*cachedChain)
			chainCacheMu.Unlock()
			chainsMu.Lock()
			cfg.Chains = newCfg.Chains
			userChains.Store(newChains)
			chainsMu.Unlock()
			infoLog.Printf("reloaded %d chains", len(newChains))
		}
	}()
}
