# SocksStrata

Simple SOCKS5 proxy server written in Go. It implements the CONNECT command
and supports optional username/password authentication with optional upstream
proxy chaining.

## Configuration

Configuration is stored in a YAML file with two sections:

* **general** – listener and logging settings
* **chains** – list of user credentials and their proxy chains

Each entry in `chains` defines the username/password a client must supply
and a sequence of upstream SOCKS5 proxies (hops) through which traffic is
forwarded. If `chains` is empty, authentication is not required and
connections are made directly.

Example configuration:

```
general:
  bind: "0.0.0.0"
  port: 1080
  log_level: "info"
  log_format: "text"

chains:
  - username: "user"
    password: "pass"
    chain:
      - name: "proxy1"
        username: "puser"
        password: "ppass"
        host: "proxy.example"
        port: 1080
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

