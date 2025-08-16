package main

import (
	"context"
	"flag"
	"log"
	"math/rand"
	"net"
	"strconv"
	"time"
)

func main() {
	flag.Parse()
	rand.Seed(time.Now().UnixNano())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	initProxies(&cfg)
	initLoggers(cfg.General.LogLevel, cfg.General.LogFormat)
	addr := net.JoinHostPort(cfg.General.Bind, strconv.Itoa(cfg.General.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	infoLog.Printf("listening on %s", addr)
	userChains := make(map[string]UserChain)
	for _, uc := range cfg.Chains {
		userChains[uc.Username] = uc
	}
	startHealthChecks(ctx, &cfg)
	startChainCacheCleanup(cfg.General.ChainCleanupInterval)
	for {
		c, err := ln.Accept()
		if err != nil {
			warnLog.Printf("accept: %v", err)
			continue
		}
		if ra, ok := c.RemoteAddr().(*net.TCPAddr); ok {
			infoLog.Printf("client connected: %s", ra.IP)
		} else {
			infoLog.Printf("client connected: %s", c.RemoteAddr())
		}
		go handleConn(c, userChains)
	}
}
