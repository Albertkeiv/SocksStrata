package main

import (
	"context"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

type testLogBuffer struct {
	sync.Mutex
	strings.Builder
}

func (b *testLogBuffer) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.Builder.Write(p)
}

func (b *testLogBuffer) String() string {
	b.Lock()
	defer b.Unlock()
	return b.Builder.String()
}

func TestStartHealthChecksUpdatesAliveAndLogs(t *testing.T) {
	tests := []struct {
		name     string
		initial  bool
		aliveRet bool
		logText  string
	}{
		{name: "recovered", initial: false, aliveRet: true, logText: "proxy p1 recovered"},
		{name: "marked dead", initial: true, aliveRet: false, logText: "proxy p1 marked dead"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf testLogBuffer
			origInfo, origWarn := infoLog, warnLog
			infoLog = log.New(&buf, "", 0)
			warnLog = log.New(&buf, "", 0)
			defer func() {
				infoLog = origInfo
				warnLog = origWarn
			}()

			p := &Proxy{Name: "p1"}
			if tt.initial {
				p.alive.Store(true)
			}
			cfg := &Config{
				General: General{
					HealthCheckInterval:   10 * time.Millisecond,
					HealthCheckTimeout:    10 * time.Millisecond,
					HealthCheckConcurrent: 1,
				},
				Chains: []UserChain{{Chain: []*Hop{{Proxies: []*Proxy{p}}}}},
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			origCheck := checkProxyAlive
			checkProxyAlive = func(context.Context, *Proxy, time.Duration) (bool, error) {
				return tt.aliveRet, nil
			}
			defer func() { checkProxyAlive = origCheck }()

			startHealthChecks(ctx, cfg)

			time.Sleep(50 * time.Millisecond)

			if p.alive.Load() != tt.aliveRet {
				t.Fatalf("expected alive=%v got %v", tt.aliveRet, p.alive.Load())
			}
			if !strings.Contains(buf.String(), tt.logText) {
				t.Fatalf("expected log %q, got %q", tt.logText, buf.String())
			}
		})
	}
}
