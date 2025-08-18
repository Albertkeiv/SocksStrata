package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strconv"
	"time"
)

func buildUserChains(chains []UserChain) (map[string]*ChainState, error) {
	userChains := make(map[string]*ChainState)
	for _, uc := range chains {
		if _, ok := userChains[uc.Username]; ok {
			return nil, fmt.Errorf("duplicate username %q", uc.Username)
		}
		userChains[uc.Username] = &ChainState{chain: uc.Chain, password: uc.Password}
	}
	return userChains, nil
}

func main() {
	flag.Parse()
	rand.Seed(time.Now().UnixNano())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	ioTimeout = cfg.General.IOTimeout
	idleTimeout = cfg.General.IdleTimeout
	initProxies(&cfg)
	initLoggers(cfg.General.LogLevel, cfg.General.LogFormat)
	addr := net.JoinHostPort(cfg.General.Bind, strconv.Itoa(cfg.General.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	sem := make(chan struct{}, cfg.General.MaxConnections)
	infoLog.Printf("listening on %s", addr)
	ucMap, err := buildUserChains(cfg.Chains)
	if err != nil {
		log.Fatal(err)
	}
	userChains.Store(ucMap)
	startHealthChecks(ctx, &cfg)
	startChainCacheCleanup(cfg.General.ChainCleanupInterval)
	startConfigReload(ctx, &cfg)
	for {
		c, err := ln.Accept()
		if err != nil {
			warnLog.Printf("accept: %v", err)
			continue
		}
		select {
		case sem <- struct{}{}:
			if ra, ok := c.RemoteAddr().(*net.TCPAddr); ok {
				infoLog.Printf("client connected: %s", ra.IP)
			} else {
				infoLog.Printf("client connected: %s", c.RemoteAddr())
			}
			go func() {
				defer func() { <-sem }()
				handleConn(c, userChains.Load().(map[string]*ChainState))
			}()
		default:
			warnLog.Printf("too many connections; closing %s", c.RemoteAddr())
			c.Close()
		}
	}
}
