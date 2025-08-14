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

type General struct {
	Bind      string `yaml:"bind"`
	Port      int    `yaml:"port"`
	LogLevel  string `yaml:"log_level"`
	LogFormat string `yaml:"log_format"`
}

type Proxy struct {
	Name     string `yaml:"name"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
}

type Hop struct {
	Strategy string  `yaml:"strategy"`
	Proxies  []Proxy `yaml:"proxies"`
	Name     string  `yaml:"name"`
	Username string  `yaml:"username"`
	Password string  `yaml:"password"`
	Host     string  `yaml:"host"`
	Port     int     `yaml:"port"`
	rrCount  uint32  `yaml:"-"`
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

func (h *Hop) orderedProxies() []Proxy {
	var proxies []Proxy
	if len(h.Proxies) > 0 {
		proxies = make([]Proxy, len(h.Proxies))
		copy(proxies, h.Proxies)
	} else if h.Host != "" {
		proxies = []Proxy{{
			Name:     h.Name,
			Username: h.Username,
			Password: h.Password,
			Host:     h.Host,
			Port:     h.Port,
		}}
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

func generateCombos(lists [][]Proxy, depth int, current []Proxy, out *[][]Proxy) {
	if depth == len(lists) {
		comb := make([]Proxy, len(current))
		copy(comb, current)
		*out = append(*out, comb)
		return
	}
	for _, p := range lists[depth] {
		current[depth] = p
		generateCombos(lists, depth+1, current, out)
	}
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
	return cfg, nil
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
	lists := make([][]Proxy, len(chain))
	for i, hop := range chain {
		lists[i] = hop.orderedProxies()
	}
	combos := [][]Proxy{}
	generateCombos(lists, 0, make([]Proxy, len(chain)), &combos)
	var lastErr error
	for _, combo := range combos {
		var conn net.Conn
		var err error
		success := true
		for i := range combo {
			nextHost := finalHost
			nextPort := finalPort
			if i+1 < len(combo) {
				next := combo[i+1]
				nextHost = next.Host
				nextPort = next.Port
			}
			conn, err = connectProxy(conn, combo[i], nextHost, nextPort)
			if err != nil {
				if conn != nil {
					conn.Close()
				}
				lastErr = fmt.Errorf("hop %s: %w", combo[i].Name, err)
				success = false
				break
			}
			debugLog.Printf("connected to hop %s targeting %s:%d", combo[i].Name, nextHost, nextPort)
		}
		if success {
			return conn, nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no valid proxy chain")
}

func connectProxy(prev net.Conn, hop Proxy, host string, port int) (net.Conn, error) {
	addr := net.JoinHostPort(hop.Host, strconv.Itoa(hop.Port))
	var conn net.Conn
	var err error
	if prev == nil {
		debugLog.Printf("dialing hop %s at %s", hop.Name, addr)
		conn, err = net.Dial("tcp", addr)
		if err != nil {
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
