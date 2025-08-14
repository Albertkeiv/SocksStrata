# SocksStrata

Simple SOCKS5 proxy server written in Go. It implements the CONNECT command
and supports optional username/password authentication with optional upstream
proxy chaining.

## Configuration

Configuration is stored in a YAML file with two sections:

* **general** – listener and logging settings
* **chains** – list of user credentials and their proxy chains

Each entry in `chains` defines the username/password a client must supply
and an ordered sequence of hops. A hop may specify a single upstream
SOCKS5 proxy directly or provide multiple proxies with a load-balancing
strategy. When multiple proxies are listed, they are tried according to
the selected strategy until a connection succeeds. Supported strategies
are `rr` (round-robin) and `random`. Hops are traversed in the order they
are listed. If `chains` is empty, authentication is not required and
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
        host: "proxy1.example"
        port: 1080
      - strategy: "rr"
        proxies:
          - name: "proxy2"
            username: "puser2"
            password: "ppass2"
            host: "proxy2.example"
            port: 1080
          - name: "proxy3"
            username: "puser3"
            password: "ppass3"
            host: "proxy3.example"
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

