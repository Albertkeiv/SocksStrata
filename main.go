package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
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

type Hop struct {
	Name     string `yaml:"name"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
}

type UserChain struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Chain    []Hop  `yaml:"chain"`
}

type Config struct {
	General General     `yaml:"general"`
	Chains  []UserChain `yaml:"chains"`
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
	var chain []Hop
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
		conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
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

func dialChain(chain []Hop, finalHost string, finalPort int) (net.Conn, error) {
	var conn net.Conn
	var err error
	targetHost := finalHost
	targetPort := finalPort
	for i := 0; i < len(chain); i++ {
		hop := chain[i]
		if i+1 < len(chain) {
			next := chain[i+1]
			targetHost = next.Host
			targetPort = next.Port
		} else {
			targetHost = finalHost
			targetPort = finalPort
		}
		conn, err = connectHop(conn, hop, targetHost, targetPort)
		if err != nil {
			if conn != nil {
				conn.Close()
			}
			return nil, err
		}
		debugLog.Printf("connected to hop %s targeting %s:%d", hop.Name, targetHost, targetPort)
	}
	return conn, nil
}

func connectHop(prev net.Conn, hop Hop, host string, port int) (net.Conn, error) {
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
	method := byte(0x00)
	if hop.Username != "" || hop.Password != "" {
		method = 0x02
	}
	if _, err := conn.Write([]byte{0x05, 0x01, method}); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return nil, err
	}
	if buf[0] != 0x05 || buf[1] != method {
		return nil, fmt.Errorf("bad method response")
	}
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
	}
	atyp, addrBytes, err := encodeAddr(host)
	if err != nil {
		return nil, err
	}
	req := []byte{0x05, 0x01, 0x00, atyp}
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
