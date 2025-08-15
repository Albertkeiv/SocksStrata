package main

import (
	"testing"
	"time"
)

func TestValidateConfig(t *testing.T) {
	validGen := General{
		Bind:                  "127.0.0.1",
		Port:                  1080,
		HealthCheckInterval:   time.Second,
		ChainCleanupInterval:  time.Second,
		HealthCheckTimeout:    time.Second,
		HealthCheckConcurrent: 1,
	}
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "invalid general port",
			cfg: Config{General: General{
				Bind:                  "0.0.0.0",
				Port:                  70000,
				HealthCheckInterval:   time.Second,
				ChainCleanupInterval:  time.Second,
				HealthCheckTimeout:    time.Second,
				HealthCheckConcurrent: 1,
			}},
		},
		{
			name: "invalid hop port",
			cfg: Config{
				General: validGen,
				Chains: []UserChain{
					{
						Chain: []*Hop{
							{Host: "example.com", Port: 0},
						},
					},
				},
			},
		},
		{
			name: "invalid strategy",
			cfg: Config{
				General: validGen,
				Chains: []UserChain{
					{
						Chain: []*Hop{
							{
								Strategy: "bogus",
								Proxies:  []*Proxy{{Host: "proxy.example", Port: 1080}},
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateConfig(&tt.cfg); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestLoadConfigInvalid(t *testing.T) {
	if _, err := loadConfig("testdata/invalid_config.yaml"); err == nil {
		t.Fatalf("expected error")
	}
}
