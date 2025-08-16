package main

import (
	"errors"
	"io"
	"net"
	"sync"
)

// proxy copies data between a and b. When one side returns an error or EOF,
// the opposite connection is closed to ensure both sides terminate.
func proxy(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	copyConn := func(dst, src net.Conn, dir string) {
		defer wg.Done()
		if _, err := io.Copy(dst, src); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				if debugLog != nil {
					debugLog.Printf("proxy %s: %v", dir, err)
				}
			} else {
				if warnLog != nil {
					warnLog.Printf("proxy %s: %v", dir, err)
				}
			}
		}
		dst.Close()
		src.Close()
	}

	go copyConn(a, b, "b→a")
	go copyConn(b, a, "a→b")

	wg.Wait()
}
