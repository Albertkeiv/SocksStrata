package main

import (
	"context"
	"io"
	"net"
	"strconv"
	"time"
)

func handleConn(conn net.Conn, chains map[string]*ChainState) {
	defer conn.Close()
	if tcp, ok := conn.(*net.TCPConn); ok {
		tcp.SetNoDelay(true)
	}
	buf := make([]byte, 260)
	conn.SetDeadline(time.Now().Add(ioTimeout))
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		warnLog.Printf("handshake read: %v, code 0xFF", err)
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := conn.Write([]byte{0x05, 0xFF}); err != nil {
			warnLog.Printf("write: %v", err)
			conn.Close()
		}
		return
	}
	if buf[0] != 0x05 {
		warnLog.Printf("unsupported version %d, code 0xFF", buf[0])
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := conn.Write([]byte{0x05, 0xFF}); err != nil {
			warnLog.Printf("write: %v", err)
			conn.Close()
		}
		return
	}
	nmethods := int(buf[1])
	if nmethods == 0 || nmethods > 255 {
		warnLog.Printf("bad nmethods %d, code 0xFF", nmethods)
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := conn.Write([]byte{0x05, 0xFF}); err != nil {
			warnLog.Printf("write: %v", err)
			conn.Close()
		}
		return
	}
	conn.SetDeadline(time.Now().Add(ioTimeout))
	if _, err := io.ReadFull(conn, buf[:nmethods]); err != nil {
		warnLog.Printf("read methods: %v, code 0xFF", err)
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := conn.Write([]byte{0x05, 0xFF}); err != nil {
			warnLog.Printf("write: %v", err)
			conn.Close()
		}
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
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := conn.Write([]byte{0x05, 0xFF}); err != nil {
			warnLog.Printf("write: %v", err)
			conn.Close()
		}
		return
	}
	conn.SetDeadline(time.Now().Add(ioTimeout))
	if _, err := conn.Write([]byte{0x05, method}); err != nil {
		warnLog.Printf("write: %v", err)
		conn.Close()
		return
	}
	debugLog.Printf("server selected method: 0x%02X", method)
	var state *ChainState
	if method == 0x02 {
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := io.ReadFull(conn, buf[:2]); err != nil {
			warnLog.Printf("auth header: %v, code 0x01", err)
			conn.SetDeadline(time.Now().Add(ioTimeout))
			if _, err := conn.Write([]byte{0x01, 0x01}); err != nil {
				warnLog.Printf("write: %v", err)
				conn.Close()
			}
			return
		}
		if buf[0] != 0x01 {
			warnLog.Printf("bad auth version %d, code 0x01", buf[0])
			conn.SetDeadline(time.Now().Add(ioTimeout))
			if _, err := conn.Write([]byte{0x01, 0x01}); err != nil {
				warnLog.Printf("write: %v", err)
				conn.Close()
			}
			return
		}
		ulen := int(buf[1])
		if ulen == 0 || ulen > 255 {
			warnLog.Printf("bad ulen %d, code 0x01", ulen)
			conn.SetDeadline(time.Now().Add(ioTimeout))
			if _, err := conn.Write([]byte{0x01, 0x01}); err != nil {
				warnLog.Printf("write: %v", err)
				conn.Close()
			}
			return
		}
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := io.ReadFull(conn, buf[:ulen+1]); err != nil {
			warnLog.Printf("read uname and plen: %v, code 0x01", err)
			conn.SetDeadline(time.Now().Add(ioTimeout))
			if _, err := conn.Write([]byte{0x01, 0x01}); err != nil {
				warnLog.Printf("write: %v", err)
				conn.Close()
			}
			return
		}
		uname := string(buf[:ulen])
		plen := int(buf[ulen])
		if plen == 0 || plen > 255 {
			warnLog.Printf("bad plen %d, code 0x01", plen)
			conn.SetDeadline(time.Now().Add(ioTimeout))
			if _, err := conn.Write([]byte{0x01, 0x01}); err != nil {
				warnLog.Printf("write: %v", err)
				conn.Close()
			}
			return
		}
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := io.ReadFull(conn, buf[:plen]); err != nil {
			warnLog.Printf("read passwd: %v, code 0x01", err)
			conn.SetDeadline(time.Now().Add(ioTimeout))
			if _, err := conn.Write([]byte{0x01, 0x01}); err != nil {
				warnLog.Printf("write: %v", err)
				conn.Close()
			}
			return
		}
		passwd := string(buf[:plen])
		st, ok := chains[uname]
		if !ok || st.password != passwd {
			warnLog.Printf("authentication failed for user %s, code 0x01", uname)
			conn.SetDeadline(time.Now().Add(ioTimeout))
			if _, err := conn.Write([]byte{0x01, 0x01}); err != nil {
				warnLog.Printf("write: %v", err)
				conn.Close()
			}
			return
		}
		state = st
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
			warnLog.Printf("write: %v", err)
			conn.Close()
			return
		}
	}
	conn.SetDeadline(time.Now().Add(ioTimeout))
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		warnLog.Printf("read request header: %v, code 0x01", err)
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
			warnLog.Printf("write: %v", err)
			conn.Close()
		}
		return
	}
	if buf[0] != 0x05 {
		return
	}
	if buf[1] != 0x01 { // CONNECT only
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
			warnLog.Printf("write: %v", err)
			conn.Close()
		}
		return
	}
	atyp := buf[3]
	var host string
	switch atyp {
	case 0x01: // IPv4
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return
		}
		host = net.IP(buf[:4]).String()
	case 0x03: // domain
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return
		}
		dlen := int(buf[0])
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := io.ReadFull(conn, buf[:dlen]); err != nil {
			return
		}
		host = string(buf[:dlen])
	case 0x04: // IPv6
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := io.ReadFull(conn, buf[:16]); err != nil {
			return
		}
		host = net.IP(buf[:16]).String()
	default:
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
			warnLog.Printf("write: %v", err)
			conn.Close()
		}
		return
	}
	conn.SetDeadline(time.Now().Add(ioTimeout))
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	port := int(buf[0])<<8 | int(buf[1])
	dest := net.JoinHostPort(host, strconv.Itoa(port))
	debugLog.Printf("connect request to %s", dest)
	ctx, cancel := context.WithTimeout(context.Background(), ioTimeout)
	defer cancel()
	var remote net.Conn
	var err error
	if state != nil && len(state.chain) > 0 {
		state.acquire()
		defer state.release()
		remote, err = dialChain(ctx, state, host, port)
	} else {
		d := net.Dialer{}
		remote, err = d.DialContext(ctx, "tcp", dest)
	}
	if err != nil {
		warnLog.Printf("connect to %s failed: %v, code 0x04", dest, err)
		conn.SetDeadline(time.Now().Add(ioTimeout))
		if _, err := conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
			warnLog.Printf("write: %v", err)
			conn.Close()
		}
		return
	}
	if tcp, ok := remote.(*net.TCPConn); ok {
		tcp.SetNoDelay(true)
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
	conn.SetDeadline(time.Now().Add(ioTimeout))
	if _, err := conn.Write(resp); err != nil {
		warnLog.Printf("write: %v", err)
		conn.Close()
		return
	}
	conn.SetDeadline(time.Time{})
	remote.SetDeadline(time.Time{})
	debugLog.Printf("server responded with %v", resp)
	proxy(remote, conn)
}
