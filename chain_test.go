package main

import (
	"bytes"
	"net"
	"strings"
	"testing"
)

func TestEncodeAddr(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		wantAtyp byte
		wantAddr []byte
		wantErr  bool
	}{
		{
			name:     "IPv4",
			host:     "127.0.0.1",
			wantAtyp: 0x01,
			wantAddr: net.ParseIP("127.0.0.1").To4(),
			wantErr:  false,
		},
		{
			name:     "IPv6",
			host:     "2001:db8::1",
			wantAtyp: 0x04,
			wantAddr: net.ParseIP("2001:db8::1").To16(),
			wantErr:  false,
		},
		{
			name:     "domain",
			host:     "example.com",
			wantAtyp: 0x03,
			wantAddr: append([]byte{byte(len("example.com"))}, []byte("example.com")...),
			wantErr:  false,
		},
		{
			name:     "too long host",
			host:     strings.Repeat("a", 256),
			wantAtyp: 0,
			wantAddr: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			atyp, addr, err := encodeAddr(tt.host)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if atyp != tt.wantAtyp {
				t.Fatalf("atyp = %v, want %v", atyp, tt.wantAtyp)
			}
			if !bytes.Equal(addr, tt.wantAddr) {
				t.Fatalf("addr = %v, want %v", addr, tt.wantAddr)
			}
		})
	}
}
