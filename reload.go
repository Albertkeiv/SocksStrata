package main

import (
	"context"
	"reflect"
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
			chainsMu.Lock()
			oldChains := userChains.Load().(map[string]*ChainState)
			updated := make(map[string]*ChainState, len(newChains))
			for name, st := range newChains {
				if old, ok := oldChains[name]; ok {
					if reflect.DeepEqual(old.chain, st.chain) && old.password == st.password {
						updated[name] = old
					} else {
						updated[name] = st
						cleanupChain(ctx, old)
					}
				} else {
					updated[name] = st
				}
			}
			for name, old := range oldChains {
				if _, ok := updated[name]; !ok {
					cleanupChain(ctx, old)
				}
			}
			cfg.Chains = newCfg.Chains
			userChains.Store(updated)
			chainsMu.Unlock()
			infoLog.Printf("reloaded %d chains", len(updated))
		}
	}()
}

func cleanupChain(ctx context.Context, cs *ChainState) {
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			if atomic.LoadInt32(&cs.refs) <= 0 {
				cs.clearCache()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
