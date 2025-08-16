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
	defaultHealthCheckTimeout    = 5 * time.Second
	defaultHealthCheckConcurrent = 10
	defaultIOTimeout             = 5 * time.Second
)

var ioTimeout = defaultIOTimeout

type General struct {
	Bind                  string        `yaml:"bind"`
	Port                  int           `yaml:"port"`
	LogLevel              string        `yaml:"log_level"`
	LogFormat             string        `yaml:"log_format"`
	HealthCheckInterval   time.Duration `yaml:"health_check_interval"`
	ChainCleanupInterval  time.Duration `yaml:"chain_cleanup_interval"`
	HealthCheckTimeout    time.Duration `yaml:"health_check_timeout"`
	HealthCheckConcurrent int           `yaml:"health_check_concurrency"`
	IOTimeout             time.Duration `yaml:"io_timeout"`
}

type Proxy struct {
	Name     string      `yaml:"name"`
	Username string      `yaml:"username"`
	Password string      `yaml:"password"`
	Host     string      `yaml:"host"`
	Port     int         `yaml:"port"`
	Priority int         `yaml:"priority"`
	alive    atomic.Bool `yaml:"-"`
}

type Hop struct {
	Strategy   string          `yaml:"strategy"`
	Proxies    []*Proxy        `yaml:"proxies"`
	Name       string          `yaml:"name"`
	Username   string          `yaml:"username"`
	Password   string          `yaml:"password"`
	Host       string          `yaml:"host"`
	Port       int             `yaml:"port"`
	rrCount    uint32          `yaml:"-"`
	priorityRR map[int]*uint32 `yaml:"-"`
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
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
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
	if cfg.General.HealthCheckTimeout == 0 {
		cfg.General.HealthCheckTimeout = defaultHealthCheckTimeout
	}
	if cfg.General.HealthCheckConcurrent <= 0 {
		cfg.General.HealthCheckConcurrent = defaultHealthCheckConcurrent
	}
	if cfg.General.IOTimeout == 0 {
		cfg.General.IOTimeout = defaultIOTimeout
	}
	if err := validateConfig(&cfg); err != nil {
		return Config{}, err
	}
	ioTimeout = cfg.General.IOTimeout
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
	if cfg.General.ChainCleanupInterval < 0 {
		return fmt.Errorf("general.chain_cleanup_interval must be non-negative")
	}
	if cfg.General.HealthCheckTimeout <= 0 {
		return fmt.Errorf("general.health_check_timeout must be positive")
	}
	if cfg.General.HealthCheckConcurrent <= 0 {
		return fmt.Errorf("general.health_check_concurrency must be positive")
	}
	if cfg.General.IOTimeout <= 0 {
		return fmt.Errorf("general.io_timeout must be positive")
	}
	for ci, uc := range cfg.Chains {
		if len(uc.Username) > 255 {
			return fmt.Errorf("chains[%d]: username too long", ci)
		}
		if len(uc.Password) > 255 {
			return fmt.Errorf("chains[%d]: password too long", ci)
		}
		for hi, hop := range uc.Chain {
			if len(hop.Proxies) > 0 {
				strat := strings.ToLower(hop.Strategy)
				if strat != "" && strat != "rr" && strat != "random" && strat != "priority" {
					return fmt.Errorf("chains[%d].chain[%d]: invalid strategy %q", ci, hi, hop.Strategy)
				}
				for pi, p := range hop.Proxies {
					if p.Host == "" {
						return fmt.Errorf("chains[%d].chain[%d].proxies[%d]: host is required", ci, hi, pi)
					}
					if p.Port <= 0 || p.Port > 65535 {
						return fmt.Errorf("chains[%d].chain[%d].proxies[%d]: port must be between 1 and 65535", ci, hi, pi)
					}
					if len(p.Username) > 255 {
						return fmt.Errorf("chains[%d].chain[%d].proxies[%d]: username too long", ci, hi, pi)
					}
					if len(p.Password) > 255 {
						return fmt.Errorf("chains[%d].chain[%d].proxies[%d]: password too long", ci, hi, pi)
					}
				}
			} else {
				if hop.Host == "" {
					return fmt.Errorf("chains[%d].chain[%d]: host is required", ci, hi)
				}
				if hop.Port <= 0 || hop.Port > 65535 {
					return fmt.Errorf("chains[%d].chain[%d]: port must be between 1 and 65535", ci, hi)
				}
				if len(hop.Username) > 255 {
					return fmt.Errorf("chains[%d].chain[%d]: username too long", ci, hi)
				}
				if len(hop.Password) > 255 {
					return fmt.Errorf("chains[%d].chain[%d]: password too long", ci, hi)
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
			if hop.priorityRR == nil {
				hop.priorityRR = make(map[int]*uint32)
			}
			for _, p := range hop.Proxies {
				p.alive.Store(true)
				if _, ok := hop.priorityRR[p.Priority]; !ok {
					var v uint32
					hop.priorityRR[p.Priority] = &v
				}
			}
		}
	}
}
