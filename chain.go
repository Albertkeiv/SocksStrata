package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type cachedChain struct {
	combo    []*Proxy
	lastUsed time.Time
}

type ChainState struct {
	chain    []*Hop
	password string
	cacheMu  sync.RWMutex
	cache    *cachedChain
	refs     int32
}

func (cs *ChainState) acquire() { atomic.AddInt32(&cs.refs, 1) }

func (cs *ChainState) release() { atomic.AddInt32(&cs.refs, -1) }

func (cs *ChainState) clearCache() {
	cs.cacheMu.Lock()
	cs.cache = nil
	cs.cacheMu.Unlock()
}

func (h *Hop) orderedProxies() []*Proxy {
	var proxies []*Proxy
	if len(h.Proxies) > 0 {
		for _, p := range h.Proxies {
			if p.alive.Load() {
				proxies = append(proxies, p)
			}
		}
	} else if h.Host != "" {
		p := &Proxy{
			Name:     h.Name,
			Username: h.Username,
			Password: h.Password,
			Host:     h.Host,
			Port:     h.Port,
		}
		p.alive.Store(true)
		proxies = []*Proxy{p}
	}
	if len(proxies) == 0 {
		return proxies
	}
	switch strings.ToLower(h.Strategy) {
	case "random":
		rand.Shuffle(len(proxies), func(i, j int) {
			proxies[i], proxies[j] = proxies[j], proxies[i]
		})
	case "priority":
		groups := make(map[int][]*Proxy)
		var priorities []int
		for _, p := range proxies {
			pr := p.Priority
			if _, ok := groups[pr]; !ok {
				priorities = append(priorities, pr)
			}
			groups[pr] = append(groups[pr], p)
		}
		sort.Slice(priorities, func(i, j int) bool { return priorities[i] > priorities[j] })
		ordered := make([]*Proxy, 0, len(proxies))
		for _, pr := range priorities {
			grp := groups[pr]
			if len(grp) > 1 {
				if cnt, ok := h.priorityRR[pr]; ok {
					idx := atomic.AddUint32(cnt, 1) - 1
					start := int(idx % uint32(len(grp)))
					grp = append(grp[start:], grp[:start]...)
				}
			}
			ordered = append(ordered, grp...)
		}
		proxies = ordered
	default:
		idx := atomic.AddUint32(&h.rrCount, 1) - 1
		start := int(idx % uint32(len(proxies)))
		proxies = append(proxies[start:], proxies[:start]...)
	}
	return proxies
}

func dialChain(ctx context.Context, state *ChainState, finalHost string, finalPort int) (net.Conn, error) {
	state.cacheMu.RLock()
	cached := state.cache
	state.cacheMu.RUnlock()
	if cached != nil {
		if conn, err := connectThrough(ctx, cached.combo, finalHost, finalPort); err == nil {
			state.cacheMu.Lock()
			cached.lastUsed = time.Now()
			state.cacheMu.Unlock()
			return conn, nil
		}
		state.cacheMu.Lock()
		state.cache = nil
		state.cacheMu.Unlock()
	}
	chain := state.chain
	current := make([]*Proxy, len(chain))
	conn, err := dialChainRecursive(ctx, chain, 0, current, finalHost, finalPort)
	if err == nil {
		combo := append([]*Proxy(nil), current...)
		state.cacheMu.Lock()
		state.cache = &cachedChain{combo: combo, lastUsed: time.Now()}
		state.cacheMu.Unlock()
	}
	return conn, err
}

func connectThrough(ctx context.Context, combo []*Proxy, finalHost string, finalPort int) (net.Conn, error) {
	var conn net.Conn
	var err error
	for i := range combo {
		nextHost := finalHost
		nextPort := finalPort
		if i+1 < len(combo) {
			next := combo[i+1]
			nextHost = next.Host
			nextPort = next.Port
		}
		conn, err = connectProxy(ctx, conn, combo[i], nextHost, nextPort, ioTimeout)
		if err != nil {
			combo[i].alive.Store(false)
			return nil, fmt.Errorf("hop %s: %w", combo[i].Name, err)
		}
		debugLog.Printf("connected to hop %s targeting %s:%d", combo[i].Name, nextHost, nextPort)
	}
	return conn, nil
}

func dialChainRecursive(ctx context.Context, chain []*Hop, depth int, current []*Proxy, finalHost string, finalPort int) (net.Conn, error) {
	if depth == len(chain) {
		return connectThrough(ctx, current, finalHost, finalPort)
	}
	proxies := chain[depth].orderedProxies()
	var lastErr error
	for _, p := range proxies {
		current[depth] = p
		conn, err := dialChainRecursive(ctx, chain, depth+1, current, finalHost, finalPort)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no valid proxy chain")
}

func connectProxy(ctx context.Context, prev net.Conn, hop *Proxy, host string, port int, timeout time.Duration) (net.Conn, error) {
	addr := net.JoinHostPort(hop.Host, strconv.Itoa(hop.Port))
	var conn net.Conn
	var err error
	if prev == nil {
		debugLog.Printf("dialing hop %s at %s", hop.Name, addr)
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		d := net.Dialer{}
		conn, err = d.DialContext(ctx, "tcp", addr)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return nil, fmt.Errorf("dial to %s timed out after %s", addr, timeout)
			}
			return nil, err
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			tcp.SetNoDelay(true)
		}
	} else {
		conn = prev
	}
	buf := make([]byte, 512)
	methods := []byte{0x00}
	wantAuth := hop.Username != "" || hop.Password != ""
	if wantAuth {
		methods = append(methods, 0x02)
	}
	req := append([]byte{0x05, byte(len(methods))}, methods...)
	conn.SetDeadline(time.Now().Add(timeout))
	if err := writeFull(conn, req); err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Now().Add(timeout))
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		conn.Close()
		return nil, err
	}
	if buf[0] != 0x05 {
		conn.Close()
		return nil, fmt.Errorf("bad method response")
	}
	method := buf[1]
	if method == 0x02 {
		u := []byte(hop.Username)
		p := []byte(hop.Password)
		if len(u) > 255 || len(p) > 255 {
			conn.Close()
			return nil, fmt.Errorf("hop %s: username/password too long", hop.Name)
		}
		req := []byte{0x01, byte(len(u))}
		req = append(req, u...)
		req = append(req, byte(len(p)))
		req = append(req, p...)
		conn.SetDeadline(time.Now().Add(timeout))
		if err := writeFull(conn, req); err != nil {
			conn.Close()
			return nil, err
		}
		conn.SetDeadline(time.Now().Add(timeout))
		if _, err := io.ReadFull(conn, buf[:2]); err != nil {
			conn.Close()
			return nil, err
		}
		if buf[1] != 0x00 {
			conn.Close()
			return nil, fmt.Errorf("auth failed for hop %s", hop.Name)
		}
	} else if method != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("bad method response")
	}
	atyp, addrBytes, err := encodeAddr(host)
	if err != nil {
		conn.Close()
		return nil, err
	}
	req = []byte{0x05, 0x01, 0x00, atyp}
	req = append(req, addrBytes...)
	req = append(req, byte(port>>8), byte(port))
	conn.SetDeadline(time.Now().Add(timeout))
	if err := writeFull(conn, req); err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Now().Add(timeout))
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		conn.Close()
		return nil, err
	}
	if buf[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("connect failed on hop %s", hop.Name)
	}
	var skip int
	switch buf[3] {
	case 0x01:
		skip = 4
	case 0x03:
		conn.SetDeadline(time.Now().Add(timeout))
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			conn.Close()
			return nil, err
		}
		skip = int(buf[0])
	case 0x04:
		skip = 16
	default:
		conn.Close()
		return nil, fmt.Errorf("bad atyp %d", buf[3])
	}
	conn.SetDeadline(time.Now().Add(timeout))
	if _, err := io.ReadFull(conn, buf[:skip+2]); err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	debugLog.Printf("hop %s connection established", hop.Name)
	return conn, nil
}

func startChainCacheCleanup(ctx context.Context, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(ttl)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				now := time.Now()
				chains := userChains.Load().(map[string]*ChainState)
				for _, st := range chains {
					st.cacheMu.Lock()
					if st.cache != nil && now.Sub(st.cache.lastUsed) > ttl {
						st.cache = nil
					}
					st.cacheMu.Unlock()
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func encodeAddr(host string) (byte, []byte, error) {
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return 0x01, ip4, nil
		}
		return 0x04, ip.To16(), nil
	}
	if len(host) > 255 {
		return 0, nil, fmt.Errorf("host too long")
	}
	b := []byte(host)
	res := append([]byte{byte(len(b))}, b...)
	return 0x03, res, nil
}
