package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
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
	if err := validateConfig(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateConfig(cfg *Config) error {
	if cfg.General.Bind == "" {
		return fmt.Errorf("general.bind is required")
	}
	if cfg.General.Port <= 0 || cfg.General.Port > 65535 {
		return fmt.Errorf("general.port must be between 1 and 65535")
	}
	if cfg.General.HealthCheckInterval <= 0 {
		return fmt.Errorf("general.health_check_interval must be positive")
	}
	if cfg.General.ChainCleanupInterval <= 0 {
		return fmt.Errorf("general.chain_cleanup_interval must be positive")
	}
	if cfg.General.HealthCheckTimeout <= 0 {
		return fmt.Errorf("general.health_check_timeout must be positive")
	}
	if cfg.General.HealthCheckConcurrent <= 0 {
		return fmt.Errorf("general.health_check_concurrency must be positive")
	}
	for ci, uc := range cfg.Chains {
		for hi, hop := range uc.Chain {
			if len(hop.Proxies) > 0 {
				strat := strings.ToLower(hop.Strategy)
				if strat != "" && strat != "rr" && strat != "random" {
					return fmt.Errorf("chains[%d].chain[%d]: invalid strategy %q", ci, hi, hop.Strategy)
				}
				for pi, p := range hop.Proxies {
					if p.Host == "" {
						return fmt.Errorf("chains[%d].chain[%d].proxies[%d]: host is required", ci, hi, pi)
					}
					if p.Port <= 0 || p.Port > 65535 {
						return fmt.Errorf("chains[%d].chain[%d].proxies[%d]: port must be between 1 and 65535", ci, hi, pi)
					}
				}
			} else {
				if hop.Host == "" {
					return fmt.Errorf("chains[%d].chain[%d]: host is required", ci, hi)
				}
				if hop.Port <= 0 || hop.Port > 65535 {
					return fmt.Errorf("chains[%d].chain[%d]: port must be between 1 and 65535", ci, hi)
				}
			}
		}
	}
	return nil
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
