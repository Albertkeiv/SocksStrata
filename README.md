# SocksStrata

Simple SOCKS5 proxy server written in Go. It implements the CONNECT command
and requires username/password authentication.

## Configuration

Configuration is stored in a YAML file:

```
bind: "0.0.0.0"
port: 1080
username: "user"
password: "pass"
```

## Usage

```
go run . -config config.yaml
```

The server listens on the configured address and forwards TCP traffic after
successful authentication.
