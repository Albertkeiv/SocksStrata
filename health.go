package main

import (
	"context"
	"net"
	"strconv"
	"sync"
	"time"
)

var checkProxyAlive = func(ctx context.Context, p *Proxy, timeout time.Duration) (bool, error) {
	addr := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false, err
	}
	conn.Close()
	return true, nil
}

func startHealthChecks(ctx context.Context, cfg *Config) {
	go func() {
		ticker := time.NewTicker(cfg.General.HealthCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			proxies := []*Proxy{}
			chainsMu.RLock()
			for i := range cfg.Chains {
				for j := range cfg.Chains[i].Chain {
					proxies = append(proxies, cfg.Chains[i].Chain[j].Proxies...)
				}
			}
			chainsMu.RUnlock()
			var wg sync.WaitGroup
			sem := make(chan struct{}, cfg.General.HealthCheckConcurrent)
			for _, p := range proxies {
				p := p
				wg.Add(1)
				go func() {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					checkCtx, cancel := context.WithTimeout(ctx, cfg.General.HealthCheckTimeout)
					defer cancel()
					alive, err := checkProxyAlive(checkCtx, p, cfg.General.HealthCheckTimeout)
					if err != nil {
						warnLog.Printf("proxy %s health check error: %v", p.Name, err)
					}
					old := p.alive.Load()
					if alive != old {
						if alive {
							infoLog.Printf("proxy %s recovered", p.Name)
						} else {
							warnLog.Printf("proxy %s marked dead", p.Name)
						}
						p.alive.Store(alive)
					}
				}()
			}
			wg.Wait()
		}
	}()
}
