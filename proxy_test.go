package main

import (
	"bytes"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProxyClosesOnClientClose(t *testing.T) {
	c1, c2 := net.Pipe()
	done := make(chan struct{})
	go func() {
		proxy(c1, c2)
		close(done)
	}()

	msg := []byte("hello")
	go func() {
		c1.Write(msg)
		c1.Close()
	}()

	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(c2, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("unexpected message: %q", buf)
	}

	<-done

	if _, err := c2.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected c2 to be closed")
	}
}

func TestProxyClosesOnRemoteClose(t *testing.T) {
	c1, c2 := net.Pipe()
	done := make(chan struct{})
	go func() {
		proxy(c1, c2)
		close(done)
	}()

	msg := []byte("world")
	go func() {
		c2.Write(msg)
		c2.Close()
	}()

	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(c1, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("unexpected message: %q", buf)
	}

	<-done

	if _, err := c1.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected c1 to be closed")
	}
}

type logBuffer struct {
	sync.Mutex
	bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.Write(p)
}

func (b *logBuffer) String() string {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.String()
}

type errConn struct {
	net.Conn
}

func (e *errConn) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestProxyLogsErrorAToB(t *testing.T) {
	var buf logBuffer
	origWarn, origDebug := warnLog, debugLog
	warnLog = log.New(&buf, "", 0)
	debugLog = log.New(&buf, "", 0)
	defer func() { warnLog = origWarn; debugLog = origDebug }()

	c1, c2 := net.Pipe()
	b := &errConn{Conn: c2}

	done := make(chan struct{})
	go func() { proxy(c1, b); close(done) }()

	go func() {
		c1.Write([]byte("x"))
		c1.Close()
	}()

	<-done

	logs := buf.String()
	if !strings.Contains(logs, "a→b") {
		t.Fatalf("expected log for a→b direction, got %q", logs)
	}
}

func TestProxyLogsErrorBToA(t *testing.T) {
	var buf logBuffer
	origWarn, origDebug := warnLog, debugLog
	warnLog = log.New(&buf, "", 0)
	debugLog = log.New(&buf, "", 0)
	defer func() { warnLog = origWarn; debugLog = origDebug }()

	c1, c2 := net.Pipe()
	a := &errConn{Conn: c1}

	done := make(chan struct{})
	go func() { proxy(a, c2); close(done) }()

	go func() {
		c2.Write([]byte("y"))
		c2.Close()
	}()

	<-done

	logs := buf.String()
	if !strings.Contains(logs, "b→a") {
		t.Fatalf("expected log for b→a direction, got %q", logs)
	}
}

func TestProxyIdleTimeout(t *testing.T) {
	var buf logBuffer
	origWarn, origDebug := warnLog, debugLog
	warnLog = log.New(&buf, "", 0)
	debugLog = log.New(&buf, "", 0)
	origIdle := idleTimeout
	idleTimeout = 50 * time.Millisecond
	defer func() {
		warnLog = origWarn
		debugLog = origDebug
		idleTimeout = origIdle
	}()

	c1, c2 := net.Pipe()
	done := make(chan struct{})
	go func() { proxy(c1, c2); close(done) }()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("proxy did not timeout")
	}

	if _, err := c1.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected c1 to be closed")
	}
	if !strings.Contains(buf.String(), "idle timeout") {
		t.Fatalf("expected idle timeout log, got %q", buf.String())
	}
}
