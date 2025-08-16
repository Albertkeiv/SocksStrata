package main

import (
	"io"
	"net"
)

// proxy copies data between a and b. When one side returns an error or EOF,
// the opposite connection is closed to ensure both sides terminate.
func proxy(a, b net.Conn) {
	done := make(chan struct{}, 2)

	go func() {
		_, _ = io.Copy(a, b)
		a.Close()
		done <- struct{}{}
	}()

	go func() {
		_, _ = io.Copy(b, a)
		b.Close()
		done <- struct{}{}
	}()

	<-done
	<-done
}
