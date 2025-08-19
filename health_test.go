package main

import (
	"context"
	"errors"
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
		errRet   error
		expected bool
		logText  string
	}{
		{name: "recovered", initial: false, aliveRet: true, expected: true, logText: "proxy p1 recovered"},
		{name: "marked dead", initial: true, aliveRet: false, expected: false, logText: "proxy p1 marked dead"},
		{name: "health check error", initial: false, aliveRet: false, errRet: errors.New("boom"), expected: false, logText: "proxy p1 health check error"},
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
				return tt.aliveRet, tt.errRet
			}
			defer func() { checkProxyAlive = origCheck }()

			startHealthChecks(ctx, cfg)

			time.Sleep(50 * time.Millisecond)

			if p.alive.Load() != tt.expected {
				t.Fatalf("expected alive=%v got %v", tt.expected, p.alive.Load())
			}
			if !strings.Contains(buf.String(), tt.logText) {
				t.Fatalf("expected log %q, got %q", tt.logText, buf.String())
			}
		})
	}
}
