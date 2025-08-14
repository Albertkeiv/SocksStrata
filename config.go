package main

import (
	"flag"
	"os"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

var configPath = flag.String("config", "config.yaml", "path to config file")

const (
	defaultHealthCheckInterval   = 30 * time.Second
	defaultChainCleanupInterval  = 10 * time.Minute
	defaultHealthCheckTimeout    = 5 * time.Second
	defaultHealthCheckConcurrent = 10
	proxyDialTimeout             = 5 * time.Second
)

type General struct {
	Bind                  string        `yaml:"bind"`
	Port                  int           `yaml:"port"`
	LogLevel              string        `yaml:"log_level"`
	LogFormat             string        `yaml:"log_format"`
	HealthCheckInterval   time.Duration `yaml:"health_check_interval"`
	ChainCleanupInterval  time.Duration `yaml:"chain_cleanup_interval"`
	HealthCheckTimeout    time.Duration `yaml:"health_check_timeout"`
	HealthCheckConcurrent int           `yaml:"health_check_concurrency"`
}

type Proxy struct {
	Name     string      `yaml:"name"`
	Username string      `yaml:"username"`
	Password string      `yaml:"password"`
	Host     string      `yaml:"host"`
	Port     int         `yaml:"port"`
	alive    atomic.Bool `yaml:"-"`
}

type Hop struct {
	Strategy string   `yaml:"strategy"`
	Proxies  []*Proxy `yaml:"proxies"`
	Name     string   `yaml:"name"`
	Username string   `yaml:"username"`
	Password string   `yaml:"password"`
	Host     string   `yaml:"host"`
	Port     int      `yaml:"port"`
	rrCount  uint32   `yaml:"-"`
}

type UserChain struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Chain    []*Hop `yaml:"chain"`
}

type Config struct {
	General General     `yaml:"general"`
	Chains  []UserChain `yaml:"chains"`
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.General.LogLevel == "" {
		cfg.General.LogLevel = "info"
	}
	if cfg.General.LogFormat == "" {
		cfg.General.LogFormat = "text"
	}
	if cfg.General.HealthCheckInterval == 0 {
		cfg.General.HealthCheckInterval = defaultHealthCheckInterval
	}
	if cfg.General.ChainCleanupInterval == 0 {
		cfg.General.ChainCleanupInterval = defaultChainCleanupInterval
	}
	if cfg.General.HealthCheckTimeout == 0 {
		cfg.General.HealthCheckTimeout = defaultHealthCheckTimeout
	}
	if cfg.General.HealthCheckConcurrent <= 0 {
		cfg.General.HealthCheckConcurrent = defaultHealthCheckConcurrent
	}
	return cfg, nil
}

func initProxies(cfg *Config) {
	for i := range cfg.Chains {
		chain := &cfg.Chains[i]
		for j := range chain.Chain {
			hop := chain.Chain[j]
			if len(hop.Proxies) == 0 && hop.Host != "" {
				p := &Proxy{
					Name:     hop.Name,
					Username: hop.Username,
					Password: hop.Password,
					Host:     hop.Host,
					Port:     hop.Port,
				}
				p.alive.Store(true)
				hop.Proxies = []*Proxy{p}
			}
			for _, p := range hop.Proxies {
				p.alive.Store(true)
			}
		}
	}
}
