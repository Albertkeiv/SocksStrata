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
