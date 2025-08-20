package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
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

	defer func() {
		if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			warnLog.Printf("listener close: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	var wg sync.WaitGroup
	done := make(chan struct{})

	go func() {
		<-sigCh
		cancel()
		if err := ln.Close(); err != nil {
			warnLog.Printf("listener close: %v", err)
		}
		wg.Wait()
		close(done)
	}()

	sem := make(chan struct{}, cfg.General.MaxConnections)

	infoLog.Printf("listening on %s", addr)
	ucMap, err := buildUserChains(cfg.Chains)
	if err != nil {
		log.Fatal(err)
	}
	userChains.Store(ucMap)
	startHealthChecks(ctx, &cfg)
	startChainCacheCleanup(ctx, cfg.General.ChainCleanupInterval)
	startConfigReload(ctx, &cfg)
	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				break
			default:
			}
			warnLog.Printf("accept: %v", err)
			continue
		}
		if tcp, ok := c.(*net.TCPConn); ok {
			tcp.SetNoDelay(true)
		}
		select {
		case sem <- struct{}{}:
			if ra, ok := c.RemoteAddr().(*net.TCPAddr); ok {
				infoLog.Printf("client connected: %s", ra.IP)
			} else {
				infoLog.Printf("client connected: %s", c.RemoteAddr())
			}
			wg.Add(1)
			go func() {
				defer func() { <-sem }()
				defer wg.Done()
				handleConn(c, userChains.Load().(map[string]*ChainState))
			}()
		default:
			warnLog.Printf("too many connections; closing %s", c.RemoteAddr())
			c.Close()
		}
	}
	<-done
}
