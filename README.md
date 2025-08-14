# SocksStrata

Simple SOCKS5 proxy server written in Go. It implements the CONNECT command
and supports optional username/password authentication with optional upstream
proxy chaining.

## Configuration

Configuration is stored in a YAML file with two sections:

* **general** – listener, logging, and health check settings
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
  health_check_interval: 30s
  chain_cleanup_interval: 10m
  health_check_timeout: 5s
  health_check_concurrency: 10

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
### Configuration Parameters

The table below describes all fields available in the YAML configuration.

#### `general`

| Field | Description | Values | Default |
| ----- | ----------- | ------ | ------- |
| `bind` | Address for the local listener. | Any IP address or hostname. | `0.0.0.0` |
| `port` | TCP port for the listener. | 1–65535. | `1080` |
| `log_level` | Logging verbosity. | `debug`, `info`, `warn`/`warning`. | `info` |
| `log_format` | Format of log output. | `text`, `json`. | `text` |
| `health_check_interval` | How often to probe upstream proxies. Accepts Go duration strings such as `30s` or `1m`. | Any positive duration. | `30s` |
| `chain_cleanup_interval` | Frequency at which cached proxy chains are purged. | Any positive duration or `0` to disable. | `10m` |
| `health_check_timeout` | Maximum time to wait for a single proxy health check. | Any positive duration. | `5s` |
| `health_check_concurrency` | Number of proxy health checks to run in parallel. | Any positive integer. | `10` |

#### `chains`

List of user definitions. Each entry requires:

| Field | Description |
| ----- | ----------- |
| `username` | Username clients must provide. |
| `password` | Password for the user. |
| `chain` | Ordered list of hops executed after authentication. If the list is empty, the connection is made directly. |

Authentication is optional; if `chains` is omitted or empty, the server accepts unauthenticated connections and connects directly.

#### Hop fields

Each item inside a user's `chain` may take one of two forms:

1. **Single proxy hop** – specify `name`, `username`, `password`, `host`, and `port` directly.
2. **Proxy group** – provide a `proxies` array containing multiple proxy definitions and optionally a `strategy`.

Additional hop parameters:

| Field | Description | Values | Default |
| ----- | ----------- | ------ | ------- |
| `strategy` | Order in which proxies from `proxies` are attempted. | `rr` for round‑robin, `random` for random selection. | `rr` |

#### Proxy fields

Proxy definitions used either directly in a hop or within a `proxies` list include:

| Field | Description |
| ----- | ----------- |
| `name` | Optional human‑readable label. |
| `username` | Username for upstream proxy authentication. |
| `password` | Password for upstream proxy authentication. |
| `host` | Hostname or IP of the upstream proxy. |
| `port` | TCP port of the upstream proxy. |

The server performs health checks on all defined proxies at the interval specified by `health_check_interval`. When a proxy fails a check it is temporarily excluded from rotation until it becomes reachable again.

## Building

To compile the proxy as a regular binary:

```bash
go build -o socksstrata
```

To produce a statically linked binary that can be moved between machines of the same architecture:

```bash
CGO_ENABLED=0 go build -ldflags "-s -w" -o socksstrata-static
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

