package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

type logger interface {
	Printf(string, ...interface{})
}

type nopLogger struct{}

func (nopLogger) Printf(string, ...interface{}) {}

type jsonLogger struct {
	level string
}

func (l jsonLogger) Printf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	m := map[string]string{
		"level": l.level,
		"time":  time.Now().Format(time.RFC3339),
		"msg":   msg,
	}
	b, _ := json.Marshal(m)
	fmt.Println(string(b))
}

var (
	infoLog  logger
	warnLog  logger
	debugLog logger
)

const (
	defaultHealthCheckInterval  = 30 * time.Second
	defaultChainCleanupInterval = 10 * time.Minute
	proxyDialTimeout            = 5 * time.Second
)

type General struct {
	Bind                 string        `yaml:"bind"`
	Port                 int           `yaml:"port"`
	LogLevel             string        `yaml:"log_level"`
	LogFormat            string        `yaml:"log_format"`
	HealthCheckInterval  time.Duration `yaml:"health_check_interval"`
	ChainCleanupInterval time.Duration `yaml:"chain_cleanup_interval"`
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

type cachedChain struct {
	combo    []*Proxy
	lastUsed time.Time
}

var (
	chainCache   = make(map[string]*cachedChain)
	chainCacheMu sync.RWMutex
)

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
	default:
		idx := atomic.AddUint32(&h.rrCount, 1) - 1
		start := int(idx % uint32(len(proxies)))
		proxies = append(proxies[start:], proxies[:start]...)
	}
	return proxies
}

var configPath = flag.String("config", "config.yaml", "path to config file")

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

func initLoggers(level, format string) {
	lvl := strings.ToLower(level)
	fmtType := strings.ToLower(format)
	switch fmtType {
	case "json":
		infoLog = jsonLogger{level: "info"}
		warnLog = jsonLogger{level: "warn"}
		debugLog = jsonLogger{level: "debug"}
	default:
		infoLog = log.New(os.Stdout, "INFO: ", log.LstdFlags)
		warnLog = log.New(os.Stdout, "WARNING: ", log.LstdFlags)
		debugLog = log.New(os.Stdout, "DEBUG: ", log.LstdFlags)
	}
	switch lvl {
	case "debug":
	case "info":
		debugLog = nopLogger{}
	case "warn", "warning":
		infoLog = nopLogger{}
		debugLog = nopLogger{}
	default:
		debugLog = nopLogger{}
	}
}

func main() {
	flag.Parse()
	rand.Seed(time.Now().UnixNano())
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
	startHealthChecks(&cfg, cfg.General.HealthCheckInterval)
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

func handleConn(conn net.Conn, chains map[string]UserChain) {
	defer conn.Close()
	buf := make([]byte, 260)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		warnLog.Printf("handshake read: %v", err)
		return
	}
	if buf[0] != 0x05 {
		warnLog.Printf("unsupported version %d", buf[0])
		return
	}
	nmethods := int(buf[1])
	if nmethods == 0 || nmethods > 255 {
		warnLog.Printf("bad nmethods %d", nmethods)
		return
	}
	if _, err := io.ReadFull(conn, buf[:nmethods]); err != nil {
		warnLog.Printf("read methods: %v", err)
		return
	}
	debugLog.Printf("client methods: %v", buf[:nmethods])
	noAuth := len(chains) == 0
	want := byte(0x02)
	if noAuth {
		want = 0x00
	}
	method := byte(0xFF)
	for i := 0; i < nmethods; i++ {
		if buf[i] == want {
			method = want
			break
		}
	}
	if method == 0xFF {
		conn.Write([]byte{0x05, 0xFF})
		return
	}
	if _, err := conn.Write([]byte{0x05, method}); err != nil {
		return
	}
	debugLog.Printf("server selected method: 0x%02X", method)
	var chain []*Hop
	if method == 0x02 {
		if _, err := io.ReadFull(conn, buf[:2]); err != nil {
			warnLog.Printf("auth header: %v", err)
			return
		}
		if buf[0] != 0x01 {
			warnLog.Printf("bad auth version %d", buf[0])
			return
		}
		ulen := int(buf[1])
		if ulen == 0 || ulen > 255 {
			warnLog.Printf("bad ulen %d", ulen)
			return
		}
		if _, err := io.ReadFull(conn, buf[:ulen+1]); err != nil {
			warnLog.Printf("read uname and plen: %v", err)
			return
		}
		uname := string(buf[:ulen])
		plen := int(buf[ulen])
		if plen == 0 || plen > 255 {
			warnLog.Printf("bad plen %d", plen)
			return
		}
		if _, err := io.ReadFull(conn, buf[:plen]); err != nil {
			warnLog.Printf("read passwd: %v", err)
			return
		}
		passwd := string(buf[:plen])
		uc, ok := chains[uname]
		if !ok || uc.Password != passwd {
			warnLog.Printf("authentication failed for user %s", uname)
			conn.Write([]byte{0x01, 0x01})
			return
		}
		chain = uc.Chain
		if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
			return
		}
	}
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		warnLog.Printf("read request header: %v", err)
		return
	}
	if buf[0] != 0x05 {
		return
	}
	if buf[1] != 0x01 { // CONNECT only
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	atyp := buf[3]
	var host string
	switch atyp {
	case 0x01: // IPv4
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return
		}
		host = net.IP(buf[:4]).String()
	case 0x03: // domain
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return
		}
		dlen := int(buf[0])
		if _, err := io.ReadFull(conn, buf[:dlen]); err != nil {
			return
		}
		host = string(buf[:dlen])
	case 0x04: // IPv6
		if _, err := io.ReadFull(conn, buf[:16]); err != nil {
			return
		}
		host = net.IP(buf[:16]).String()
	default:
		conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	port := int(buf[0])<<8 | int(buf[1])
	dest := net.JoinHostPort(host, strconv.Itoa(port))
	debugLog.Printf("connect request to %s", dest)
	var remote net.Conn
	var err error
	if len(chain) > 0 {
		remote, err = dialChain(chain, host, port)
	} else {
		remote, err = net.Dial("tcp", dest)
	}
	if err != nil {
		warnLog.Printf("connect to %s failed: %v", dest, err)
		// Use host unreachable response for any upstream failure so
		// the client is aware that the request could not be
		// satisfied through the configured proxy chain.
		conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()
	la := remote.LocalAddr().(*net.TCPAddr)
	lip := la.IP.To4()
	atyp = 0x01
	if lip == nil {
		lip = la.IP
		atyp = 0x04
	}
	resp := []byte{0x05, 0x00, 0x00, atyp}
	resp = append(resp, lip...)
	resp = append(resp, byte(la.Port>>8), byte(la.Port))
	if _, err := conn.Write(resp); err != nil {
		return
	}
	debugLog.Printf("server responded with %v", resp)
	go io.Copy(remote, conn)
	io.Copy(conn, remote)
}

func dialChain(chain []*Hop, finalHost string, finalPort int) (net.Conn, error) {
	key := chainKey(chain)
	chainCacheMu.RLock()
	cached := chainCache[key]
	chainCacheMu.RUnlock()
	if cached != nil {
		if conn, err := connectThrough(cached.combo, finalHost, finalPort); err == nil {
			chainCacheMu.Lock()
			cached.lastUsed = time.Now()
			chainCacheMu.Unlock()
			return conn, nil
		} else {
			chainCacheMu.Lock()
			delete(chainCache, key)
			chainCacheMu.Unlock()
		}
	}
	current := make([]*Proxy, len(chain))
	conn, err := dialChainRecursive(chain, 0, current, finalHost, finalPort)
	if err == nil {
		combo := append([]*Proxy(nil), current...)
		chainCacheMu.Lock()
		chainCache[key] = &cachedChain{combo: combo, lastUsed: time.Now()}
		chainCacheMu.Unlock()
	}
	return conn, err
}

func chainKey(chain []*Hop) string {
	parts := make([]string, len(chain))
	for i, hop := range chain {
		parts[i] = fmt.Sprintf("%p", hop)
	}
	return strings.Join(parts, "-")
}

func connectThrough(combo []*Proxy, finalHost string, finalPort int) (net.Conn, error) {
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
		conn, err = connectProxy(conn, combo[i], nextHost, nextPort, proxyDialTimeout)
		if err != nil {
			combo[i].alive.Store(false)
			if conn != nil {
				conn.Close()
			}
			return nil, fmt.Errorf("hop %s: %w", combo[i].Name, err)
		}
		debugLog.Printf("connected to hop %s targeting %s:%d", combo[i].Name, nextHost, nextPort)
	}
	return conn, nil
}

func dialChainRecursive(chain []*Hop, depth int, current []*Proxy, finalHost string, finalPort int) (net.Conn, error) {
	if depth == len(chain) {
		return connectThrough(current, finalHost, finalPort)
	}
	proxies := chain[depth].orderedProxies()
	var lastErr error
	for _, p := range proxies {
		current[depth] = p
		conn, err := dialChainRecursive(chain, depth+1, current, finalHost, finalPort)
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

func connectProxy(prev net.Conn, hop *Proxy, host string, port int, timeout time.Duration) (net.Conn, error) {
	addr := net.JoinHostPort(hop.Host, strconv.Itoa(hop.Port))
	var conn net.Conn
	var err error
	if prev == nil {
		debugLog.Printf("dialing hop %s at %s", hop.Name, addr)
		conn, err = net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return nil, fmt.Errorf("dial to %s timed out after %s", addr, timeout)
			}
			return nil, err
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
	if _, err := conn.Write(req); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return nil, err
	}
	if buf[0] != 0x05 {
		return nil, fmt.Errorf("bad method response")
	}
	method := buf[1]
	if method == 0x02 {
		u := []byte(hop.Username)
		p := []byte(hop.Password)
		req := []byte{0x01, byte(len(u))}
		req = append(req, u...)
		req = append(req, byte(len(p)))
		req = append(req, p...)
		if _, err := conn.Write(req); err != nil {
			return nil, err
		}
		if _, err := io.ReadFull(conn, buf[:2]); err != nil {
			return nil, err
		}
		if buf[1] != 0x00 {
			return nil, fmt.Errorf("auth failed for hop %s", hop.Name)
		}
	} else if method != 0x00 {
		return nil, fmt.Errorf("bad method response")
	}
	atyp, addrBytes, err := encodeAddr(host)
	if err != nil {
		return nil, err
	}
	req = []byte{0x05, 0x01, 0x00, atyp}
	req = append(req, addrBytes...)
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return nil, err
	}
	if buf[1] != 0x00 {
		return nil, fmt.Errorf("connect failed on hop %s", hop.Name)
	}
	var skip int
	switch buf[3] {
	case 0x01:
		skip = 4
	case 0x03:
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return nil, err
		}
		skip = int(buf[0])
	case 0x04:
		skip = 16
	default:
		return nil, fmt.Errorf("bad atyp %d", buf[3])
	}
	if _, err := io.ReadFull(conn, buf[:skip+2]); err != nil {
		return nil, err
	}
	debugLog.Printf("hop %s connection established", hop.Name)
	return conn, nil
}

func checkProxyAlive(p *Proxy) bool {
	addr := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
	conn, err := net.DialTimeout("tcp", addr, proxyDialTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func startHealthChecks(cfg *Config, interval time.Duration) {
	proxies := []*Proxy{}
	for i := range cfg.Chains {
		for j := range cfg.Chains[i].Chain {
			proxies = append(proxies, cfg.Chains[i].Chain[j].Proxies...)
		}
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			<-ticker.C
			for _, p := range proxies {
				alive := checkProxyAlive(p)
				old := p.alive.Load()
				if alive != old {
					if alive {
						infoLog.Printf("proxy %s recovered", p.Name)
					} else {
						warnLog.Printf("proxy %s marked dead", p.Name)
					}
					p.alive.Store(alive)
				}
			}
		}
	}()
}

func startChainCacheCleanup(ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(ttl)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			chainCacheMu.Lock()
			for k, v := range chainCache {
				if now.Sub(v.lastUsed) > ttl {
					delete(chainCache, k)
				}
			}
			chainCacheMu.Unlock()
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
