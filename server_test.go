package main

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// helper to run handshake tests
func handshakeTest(t *testing.T, req, want []byte, chains map[string]*ChainState) {
	t.Helper()
	origWarn, origDebug := warnLog, debugLog
	warnLog, debugLog = nopLogger{}, nopLogger{}
	defer func() { warnLog, debugLog = origWarn, origDebug }()

	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		handleConn(server, chains)
		close(done)
	}()

	if _, err := client.Write(req); err != nil {
		t.Fatalf("write: %v", err)
	}

	resp := make([]byte, len(want))
	client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(client, resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(resp, want) {
		t.Fatalf("unexpected response %v, want %v", resp, want)
	}

	<-done

	client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected connection to be closed")
	}
	client.Close()
}

func TestHandleConnBadVersion(t *testing.T) {
	handshakeTest(t, []byte{0x04, 0x01}, []byte{0x05, 0xFF}, nil)
}

func TestHandleConnNoMethods(t *testing.T) {
	handshakeTest(t, []byte{0x05, 0x00}, []byte{0x05, 0xFF}, nil)
}

func TestHandleConnMissingMethods(t *testing.T) {
	orig := ioTimeout
	ioTimeout = 50 * time.Millisecond
	defer func() { ioTimeout = orig }()
	handshakeTest(t, []byte{0x05, 0x01}, []byte{0x05, 0xFF}, nil)
}

func TestHandleConnUnsupportedMethod(t *testing.T) {
	chains := map[string]*ChainState{"u": {}}
	handshakeTest(t, []byte{0x05, 0x01, 0x00}, []byte{0x05, 0xFF}, chains)
}

func TestHandleConnConnectNoAuth(t *testing.T) {
	origWarn, origDebug := warnLog, debugLog
	warnLog, debugLog = nopLogger{}, nopLogger{}
	defer func() { warnLog, debugLog = origWarn, origDebug }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	remoteCh := make(chan []byte, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 4)
		if _, err := io.ReadFull(c, buf); err == nil {
			remoteCh <- buf
			c.Write([]byte("pong"))
		}
	}()

	client, server := net.Pipe()
	done := make(chan struct{})
	go func() { handleConn(server, nil); close(done) }()

	// handshake
	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("handshake write: %v", err)
	}
	buf := make([]byte, 2)
	client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("handshake read: %v", err)
	}
	if buf[1] != 0x00 {
		t.Fatalf("expected method 0x00, got 0x%02X", buf[1])
	}

	// connect request
	addr := ln.Addr().(*net.TCPAddr)
	req := []byte{0x05, 0x01, 0x00, 0x01}
	req = append(req, addr.IP.To4()...)
	req = append(req, byte(addr.Port>>8), byte(addr.Port))
	if _, err := client.Write(req); err != nil {
		t.Fatalf("connect write: %v", err)
	}
	resp := make([]byte, 10)
	client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(client, resp); err != nil {
		t.Fatalf("connect read: %v", err)
	}
	if resp[1] != 0x00 {
		t.Fatalf("expected response 0x00, got 0x%02X", resp[1])
	}

	// data proxying
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("data write: %v", err)
	}
	got := <-remoteCh
	if string(got) != "ping" {
		t.Fatalf("remote got %q, want %q", got, "ping")
	}
	buf = make([]byte, 4)
	client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("data read: %v", err)
	}
	if string(buf) != "pong" {
		t.Fatalf("unexpected data %q", buf)
	}

	client.Close()
	<-done
}

func TestHandleConnConnectWithAuth(t *testing.T) {
	origWarn, origDebug := warnLog, debugLog
	warnLog, debugLog = nopLogger{}, nopLogger{}
	defer func() { warnLog, debugLog = origWarn, origDebug }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	remoteCh := make(chan []byte, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 4)
		if _, err := io.ReadFull(c, buf); err == nil {
			remoteCh <- buf
			c.Write([]byte("pong"))
		}
	}()

	chains := map[string]*ChainState{"user": {password: "pass"}}
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() { handleConn(server, chains); close(done) }()

	// handshake with auth
	if _, err := client.Write([]byte{0x05, 0x01, 0x02}); err != nil {
		t.Fatalf("handshake write: %v", err)
	}
	buf := make([]byte, 2)
	client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("handshake read: %v", err)
	}
	if buf[1] != 0x02 {
		t.Fatalf("expected method 0x02, got 0x%02X", buf[1])
	}

	// auth
	auth := []byte{0x01, 0x04}
	auth = append(auth, []byte("user")...)
	auth = append(auth, 0x04)
	auth = append(auth, []byte("pass")...)
	if _, err := client.Write(auth); err != nil {
		t.Fatalf("auth write: %v", err)
	}
	client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("auth read: %v", err)
	}
	if buf[1] != 0x00 {
		t.Fatalf("auth failed: 0x%02X", buf[1])
	}

	// connect request
	addr := ln.Addr().(*net.TCPAddr)
	req := []byte{0x05, 0x01, 0x00, 0x01}
	req = append(req, addr.IP.To4()...)
	req = append(req, byte(addr.Port>>8), byte(addr.Port))
	if _, err := client.Write(req); err != nil {
		t.Fatalf("connect write: %v", err)
	}
	resp := make([]byte, 10)
	client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(client, resp); err != nil {
		t.Fatalf("connect read: %v", err)
	}
	if resp[1] != 0x00 {
		t.Fatalf("expected response 0x00, got 0x%02X", resp[1])
	}

	// data proxying
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("data write: %v", err)
	}
	got := <-remoteCh
	if string(got) != "ping" {
		t.Fatalf("remote got %q, want %q", got, "ping")
	}
	buf = make([]byte, 4)
	client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("data read: %v", err)
	}
	if string(buf) != "pong" {
		t.Fatalf("unexpected data %q", buf)
	}

	client.Close()
	<-done
}

func TestHandleConnConnectFail(t *testing.T) {
	origWarn, origDebug := warnLog, debugLog
	warnLog, debugLog = nopLogger{}, nopLogger{}
	orig := ioTimeout
	ioTimeout = 100 * time.Millisecond
	defer func() {
		warnLog, debugLog = origWarn, origDebug
		ioTimeout = orig
	}()

	client, server := net.Pipe()
	done := make(chan struct{})
	go func() { handleConn(server, nil); close(done) }()

	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("handshake write: %v", err)
	}
	buf := make([]byte, 2)
	client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("handshake read: %v", err)
	}
	if buf[1] != 0x00 {
		t.Fatalf("expected method 0x00, got 0x%02X", buf[1])
	}

	ip := net.ParseIP("203.0.113.1").To4()
	req := []byte{0x05, 0x01, 0x00, 0x01}
	req = append(req, ip...)
	req = append(req, 0, 1)
	if _, err := client.Write(req); err != nil {
		t.Fatalf("connect write: %v", err)
	}
	resp := make([]byte, 10)
	client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(client, resp); err != nil {
		t.Fatalf("connect read: %v", err)
	}
	if resp[1] != 0x04 {
		t.Fatalf("expected response 0x04, got 0x%02X", resp[1])
	}

	client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected connection to close")
	}
	client.Close()
	<-done
}
