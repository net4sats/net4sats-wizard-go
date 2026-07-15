# net4sats wizard

Cross-platform onboarding wizard that turns an OpenWrt router into a Bitcoin
WiFi access point. Single Go binary — serves a web UI that auto-discovers
routers on your LAN and deploys net4sats over SSH.

```
┌─ Your Laptop ─────────────────────────────┐
│                                           │
│  net4sats-wizard binary                   │
│  └─ web UI at http://localhost:8099       │
│     └─ scans LAN for routers              │
│     └─ deploys via SSH                    │
│                                           │
└──────────┬────────────────────────────────┘
           │ SSH
           ▼
┌─ Router (OpenWrt) ────────────────────────┐
│  tollgate-wrt   (:2121)  payment backend  │
│  nodogsplash    (:2050)  captive portal    │
│  configurationwizzard (:8090) admin panel  │
└───────────────────────────────────────────┘
```

## Quick start

1. **Flash your router to OpenWrt** (wizard does NOT flash firmware). For
   GL.iNet routers, use the web UI at `http://192.168.8.1` → Advanced →
   Upload Firmware → untick "Keep settings".

2. **Set a root password:**
   ```sh
   ssh root@192.168.1.1
   passwd
   ```

3. **Download the wizard** from the
   [latest release](https://github.com/net4sats/net4sats-wizard-go/releases/latest):

   | OS | File |
   |---|---|
   | macOS (Intel) | `net4sats-wizard-darwin-amd64` |
   | macOS (Apple Silicon) | `net4sats-wizard-darwin-arm64` |
   | Linux (x86_64) | `net4sats-wizard-linux-amd64` |
   | Windows | `net4sats-wizard-windows-amd64.exe` |

4. **Run it:**
   ```sh
   chmod +x net4sats-wizard-*
   ./net4sats-wizard-darwin-arm64   # replace with your OS
   ```

5. **Browser opens at** `http://localhost:8099` — follow the wizard:
   - It scans your network and lists detected routers
   - Select your router, enter the root password
   - Choose upstream connection (Ethernet WAN or WiFi repeater)
   - Enter your Lightning address (where payouts go)
   - Click "Deploy net4sats"

6. After ~30 seconds: connect to the `net4sats-portal` WiFi and open any
   website — the captive portal appears with payment options.

## What the wizard does

The deployment runs 9 steps over SSH:

| # | Step | Action |
|---|------|--------|
| 1 | Verify SSH | Connects, reads `/etc/openwrt_release` |
| 2 | Check firmware | Parses OpenWrt version |
| 3 | Set password | Sets the root password you entered |
| 4 | Configure upstream | WiFi STA mode or WAN passthrough |
| 5 | Install net4sats | `apk add net4sats` (pulls tollgate + nodogsplash) |
| 6 | Brand portal | Sets gateway name to `net4sats` |
| 7 | Configure Lightning | Sets your Lightning address, dev split, margin, mint |
| 8 | Restart services | Restarts `tollgate-wrt` + `nodogsplash` |
| 9 | Health check | Verifies TollGate API responding on `:2121` |

## Verify binaries

```sh
sha256sum -c SHA256SUMS
```

Checksums are published with each [release](https://github.com/net4sats/net4sats-wizard-go/releases).

## Build from source

```sh
git clone https://github.com/net4sats/net4sats-wizard-go.git
cd net4sats-wizard-go
go build -o net4sats-wizard .
./net4sats-wizard
```

Cross-compile for all platforms:

```sh
GOOS=darwin  GOARCH=arm64 go build -o dist/net4sats-wizard-darwin-arm64 .
GOOS=darwin  GOARCH=amd64 go build -o dist/net4sats-wizard-darwin-amd64 .
GOOS=linux   GOARCH=amd64 go build -o dist/net4sats-wizard-linux-amd64 .
GOOS=windows GOARCH=amd64 go build -o dist/net4sats-wizard-windows-amd64.exe .
```

## Documentation

- **[Setup guide (step by step)](https://github.com/net4sats/net4sats.github.io/pull/1)** —
  full walkthrough with screenshots, troubleshooting, and advanced settings
  (dev split, margin, mint selection)
- **[Admin panel guide](https://github.com/net4sats/net4sats.github.io/pull/1)** —
  using the configurationwizzard admin dashboard after deployment (dashboard,
  WiFi, devices, settings, wallet, identity)
- **[Endo onboarding runbook](docs/endo-runbook.md)** — operator-facing setup
  guide tested on a live GL-MT6000

## Prerequisites

- **Router** running OpenWrt (24.10.x or 25.x). The wizard does NOT flash
  firmware — see GL.iNet or OpenWrt docs for flashing.
- **SSH access** — port 22 open, root password set.
- **Upstream internet** — either Ethernet cable into WAN port, or WiFi
  credentials for the router to join an existing network.
- **Lightning address** — where Bitcoin payments from customers route
  (e.g. `you@walletofsatoshi.com` or a raw LNURL).

## Architecture

This is a **thin UI wrapper**. All business logic lives in
[tollgate-module-basic-go](https://github.com/net4sats/tollgate-module-basic-go).
The wizard only discovers routers, renders a deployment UI, and runs SSH
commands — no payment processing, identity derivation, or Nostr logic.

| Repo | Role |
|------|------|
| **net4sats-wizard-go** (this repo) | Laptop-side onboarding wizard |
| [configurationwizzard](https://github.com/net4sats/configurationwizzard) | Router-side admin panel + captive portal |
| [tollgate-module-basic-go](https://github.com/net4sats/tollgate-module-basic-go) | Payment backend (Cashu + Lightning) |

## License

MIT
