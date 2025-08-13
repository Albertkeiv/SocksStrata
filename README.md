# SocksStrata

Simple SOCKS5 proxy server written in Go. It implements the CONNECT command
and supports optional username/password authentication.

## Configuration

Configuration is stored in a YAML file with the following keys:

| Key | Description |
| --- | --- |
| `bind` | Address the proxy listens on. |
| `port` | Port number for incoming connections. |
| `username` | Optional username required for clients. |
| `password` | Optional password required for clients. |
| `log_level` | Minimum log level (`debug`, `info`, `warn`). |
| `log_format` | Log output format (`text` or `json`). |

Omit `username` and `password` to allow connections without authentication:

```
bind: "0.0.0.0"
port: 1080
username: "user"
password: "pass"
log_level: "info"
log_format: "text"
```

## Usage

```
go run . -config config.yaml
```

The server listens on the configured address and forwards TCP traffic after
authentication if credentials are configured.

## Logging

Logging verbosity is controlled by `log_level` and the output format by
`log_format`. The proxy emits the following levels:

- **INFO**: client connections, including the client's IP address.
- **WARNING**: non-critical errors and authentication failures.
- **DEBUG**: detailed information such as supported methods and server responses.

