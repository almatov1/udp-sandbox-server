# UDP Sandbox Server (Go)

A minimal authoritative UDP server.

## Requirements

- Go 1.24 or later
- UDP port 9000 must be accessible through the firewall

## Running

```bash
go run ./cmd/server
```

Build:

```bash
go build -o udp-sandbox-server ./cmd/server
./udp-sandbox-server
```

## Protocol

The client sends JSON over UDP to port 9000:

```json
{"type":"input","buttons":8}
```

Button bit values:

| Button | Value |
|---|---:|
| W / Up | 1 |
| S / Down | 2 |
| A / Left | 4 |
| D / Right | 8 |
| None | 0 |

Bit values can be combined. For example, `9` means up + right.

The server broadcasts a world snapshot approximately 20 times per second:

```json
{
  "type": "snapshot",
  "players": [
    {"id": 1, "x": 120.0, "y": 200.0, "color": "#FF5555"}
  ]
}
```

The client should regularly send the current button mask (e.g., 20–30 times per second), including `0` when all buttons are released.

## Notes

- Player positions are calculated by the server; the client does not send coordinates.
- Movement speed is 600 px/s, world size is 800x600, and player size is 40x40.
- Movement input is reset if no input update is received for 100 ms.
- A player is removed after 2 seconds without receiving any packets.
- A session is identified by its UDP IP:port pair. NAT rebinding and secure identity verification are not implemented.
- This is a basic example, not a secure public production server. JSON is convenient for getting started, but compact binary protocols are generally used for high-load applications.
