package main

import (
	"bufio"
	"bytes"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Bind     string
	Port     int
	Username string
	Password string
}

var configPath = flag.String("config", "config.yaml", "path to config file")

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
		switch key {
		case "bind":
			cfg.Bind = value
		case "port":
			p, err := strconv.Atoi(value)
			if err != nil {
				return Config{}, err
			}
			cfg.Port = p
		case "username":
			cfg.Username = value
		case "password":
			cfg.Password = value
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func main() {
	flag.Parse()
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	addr := net.JoinHostPort(cfg.Bind, strconv.Itoa(cfg.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("listening on %s", addr)
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Println("accept:", err)
			continue
		}
		go handleConn(c, cfg.Username, cfg.Password)
	}
}

func handleConn(conn net.Conn, user, pass string) {
	defer conn.Close()
	buf := make([]byte, 260)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		log.Println("handshake read:", err)
		return
	}
	if buf[0] != 0x05 {
		log.Println("unsupported version", buf[0])
		return
	}
	nmethods := int(buf[1])
	if nmethods == 0 || nmethods > 255 {
		log.Println("bad nmethods", nmethods)
		return
	}
	if _, err := io.ReadFull(conn, buf[:nmethods]); err != nil {
		log.Println("read methods:", err)
		return
	}
	method := byte(0xFF)
	for i := 0; i < nmethods; i++ {
		if buf[i] == 0x02 {
			method = 0x02
			break
		}
	}
	if method != 0x02 {
		conn.Write([]byte{0x05, 0xFF})
		return
	}
	if _, err := conn.Write([]byte{0x05, 0x02}); err != nil {
		return
	}
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		log.Println("auth header:", err)
		return
	}
	if buf[0] != 0x01 {
		log.Println("bad auth version", buf[0])
		return
	}
	ulen := int(buf[1])
	if ulen == 0 || ulen > 255 {
		log.Println("bad ulen", ulen)
		return
	}
	if _, err := io.ReadFull(conn, buf[:ulen+1]); err != nil {
		log.Println("read uname and plen:", err)
		return
	}
	uname := string(buf[:ulen])
	plen := int(buf[ulen])
	if plen == 0 || plen > 255 {
		log.Println("bad plen", plen)
		return
	}
	if _, err := io.ReadFull(conn, buf[:plen]); err != nil {
		log.Println("read passwd:", err)
		return
	}
	passwd := string(buf[:plen])
	if uname != user || passwd != pass {
		conn.Write([]byte{0x01, 0x01})
		return
	}
	if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
		return
	}
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		log.Println("read request header:", err)
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
	remote, err := net.Dial("tcp", dest)
	if err != nil {
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
	go io.Copy(remote, conn)
	io.Copy(conn, remote)
}
