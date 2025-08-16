package main

import (
	"io"
	"net"
	"strconv"
)

func handleConn(conn net.Conn, chains map[string]UserChain) {
	defer conn.Close()
	buf := make([]byte, 260)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		warnLog.Printf("handshake read: %v, code 0xFF", err)
		conn.Write([]byte{0x05, 0xFF})
		return
	}
	if buf[0] != 0x05 {
		warnLog.Printf("unsupported version %d, code 0xFF", buf[0])
		conn.Write([]byte{0x05, 0xFF})
		return
	}
	nmethods := int(buf[1])
	if nmethods == 0 || nmethods > 255 {
		warnLog.Printf("bad nmethods %d, code 0xFF", nmethods)
		conn.Write([]byte{0x05, 0xFF})
		return
	}
	if _, err := io.ReadFull(conn, buf[:nmethods]); err != nil {
		warnLog.Printf("read methods: %v, code 0xFF", err)
		conn.Write([]byte{0x05, 0xFF})
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
			warnLog.Printf("auth header: %v, code 0x01", err)
			conn.Write([]byte{0x01, 0x01})
			return
		}
		if buf[0] != 0x01 {
			warnLog.Printf("bad auth version %d, code 0x01", buf[0])
			conn.Write([]byte{0x01, 0x01})
			return
		}
		ulen := int(buf[1])
		if ulen == 0 || ulen > 255 {
			warnLog.Printf("bad ulen %d, code 0x01", ulen)
			conn.Write([]byte{0x01, 0x01})
			return
		}
		if _, err := io.ReadFull(conn, buf[:ulen+1]); err != nil {
			warnLog.Printf("read uname and plen: %v, code 0x01", err)
			conn.Write([]byte{0x01, 0x01})
			return
		}
		uname := string(buf[:ulen])
		plen := int(buf[ulen])
		if plen == 0 || plen > 255 {
			warnLog.Printf("bad plen %d, code 0x01", plen)
			conn.Write([]byte{0x01, 0x01})
			return
		}
		if _, err := io.ReadFull(conn, buf[:plen]); err != nil {
			warnLog.Printf("read passwd: %v, code 0x01", err)
			conn.Write([]byte{0x01, 0x01})
			return
		}
		passwd := string(buf[:plen])
		uc, ok := chains[uname]
		if !ok || uc.Password != passwd {
			warnLog.Printf("authentication failed for user %s, code 0x01", uname)
			conn.Write([]byte{0x01, 0x01})
			return
		}
		chain = uc.Chain
		if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
			return
		}
	}
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		warnLog.Printf("read request header: %v, code 0x01", err)
		conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
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
		warnLog.Printf("connect to %s failed: %v, code 0x04", dest, err)
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
	proxy(remote, conn)
}
