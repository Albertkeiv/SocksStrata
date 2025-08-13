# SocksStrata

Simple SOCKS5 proxy server written in Go. It implements the CONNECT command
and requires username/password authentication.

## Usage

```
go run . -addr :1080 -user myuser -pass mypass
```

The server listens on the provided address and forwards TCP traffic after
successful authentication.
