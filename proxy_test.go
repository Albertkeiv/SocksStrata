package main

import (
	"io"
	"net"
	"testing"
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
