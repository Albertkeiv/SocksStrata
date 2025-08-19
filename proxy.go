package main

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// proxy copies data between a and b. When one side returns an error or EOF,
// the opposite connection is closed to ensure both sides terminate.
func proxy(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	copyConn := func(dst, src net.Conn, dir string) {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		dst.SetWriteDeadline(time.Now().Add(idleTimeout))
		src.SetReadDeadline(time.Now().Add(idleTimeout))
		for {
			n, err := src.Read(buf)
			if n > 0 {
				if _, werr := dst.Write(buf[:n]); werr != nil {
					if ne, ok := werr.(net.Error); ok && ne.Timeout() {
						if warnLog != nil {
							warnLog.Printf("proxy %s: idle timeout", dir)
						}
					} else if !errors.Is(werr, net.ErrClosed) {
						if warnLog != nil {
							warnLog.Printf("proxy %s: %v", dir, werr)
						}
					}
					break
				}
				src.SetReadDeadline(time.Now().Add(idleTimeout))
				dst.SetWriteDeadline(time.Now().Add(idleTimeout))
			}
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					if warnLog != nil {
						warnLog.Printf("proxy %s: idle timeout", dir)
					}
				} else if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
					if debugLog != nil {
						debugLog.Printf("proxy %s: %v", dir, err)
					}
				} else {
					if warnLog != nil {
						warnLog.Printf("proxy %s: %v", dir, err)
					}
				}
				break
			}
		}
		dst.Close()
		src.Close()
	}

	go copyConn(a, b, "b→a")
	go copyConn(b, a, "a→b")

	wg.Wait()
}
