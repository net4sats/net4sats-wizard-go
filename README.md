# net4sats wizard

A cross-platform desktop tool that discovers OpenWrt routers on your local
network and deploys [net4sats](https://github.com/orgs/net4sats/repositories)
— Bitcoin paid-WiFi with Lightning and Cashu ecash.

The wizard is a single Go binary that serves a web UI at
`http://localhost:8099`. It scans your network, finds routers, and walks
you through deploying net4sats over SSH — no CLI knowledge required.

![net4sats wizard flow](https://github.com/net4sats/net4sats-wizard-go/raw/main/docs/wizard-flow.png)

---

## Quick start

### 1. Download

Grab the binary for your OS from the
[latest release](https://github.com/net4sats/net4sats-wizard-go/releases/latest):

| Your laptop | File |
|---|---|
| Linux (x86_64) | `net4sats-wizard-linux-amd64` |
| Windows | `net4sats-wizard-windows-amd64.exe` |
| macOS (Intel) | `net4sats-wizard-darwin-amd64` |
| macOS (Apple Silicon M1/M2/M3) | `net4sats-wizard-darwin-arm64` |

### 2. Run

**macOS / Linux:**
```sh
chmod +x net4sats-wizard-*
./net4sats-wizard-linux-amd64   # or your platform
```

**Windows:** Double-click `net4sats-wizard-windows-amd64.exe`
(SmartScreen may warn — choose "Run anyway"; the binary is unsigned.)

### 3. Open the wizard

Open `http://localhost:8099` in your browser. The wizard will:

1. **Scan** your network for routers (ARP table + SSH probing)
2. **Select** your router from the discovered list
3. **Configure** — enter root password, Lightning address, upstream connection
4. **Deploy** — 9 automated SSH steps install and configure everything
5. **Done** — your router is a Bitcoin WiFi hotspot

The whole process takes about 30 seconds after you click Deploy.

---

## Prerequisites

- **Router must already run OpenWrt** (24.10.x or 25.x). The wizard
  configures an existing OpenWrt installation — it does not flash firmware.
  If your router still runs stock firmware, flash OpenWrt first.
- **SSH open** on the router (port 22).
- **Same network** — your laptop and router must be on the same subnet
  (WiFi or ethernet).
- **Lightning address** (e.g. `you@wallet.app`) or raw LNURL. This is
  where Bitcoin payments route. Required.

---

## What the deploy does

9 steps, all over SSH:

| Step | Action |
|------|--------|
| 1. Verify | Connects via SSH, confirms OpenWrt firmware |
| 2. Firmware | Reads and displays the firmware version |
| 3. Password | Sets the router root password |
| 4. Upstream | Configures WAN (ethernet) or WiFi STA (repeater) |
| 5. Install | Installs the `net4sats` package (apk/opkg) |
| 6. Brand | Sets captive portal name to "net4sats" |
| 7. Lightning | Writes your Lightning address + dev split + margin + mint to config |
| 8. Services | Restarts tollgate-wrt and nodogsplash |
| 9. Health | Verifies the TollGate API is responding on :2121 |

---

## Advanced settings

Available behind the "Advanced settings" dropdown in the wizard UI:

| Setting | Default | Description |
|---------|---------|-------------|
| Dev split | 10% | Share of payments to the net4sats developer fund (0-50%) |
| Margin | 0% | Operator markup on top of data costs (0-100%) |
| Cashu mint | 8333.space | Preferred Cashu mint for ecash payments |

---

## After deployment

Once the wizard shows "net4sats is live!", you can:

- **Admin panel** — open `http://<router-ip>/` in a browser. Login with
  the root password. See the
  [Admin Panel Guide](https://github.com/net4sats/net4sats.github.io/blob/docs/operator-guides/docs/admin-panel-guide.md)
  for a full walkthrough of the dashboard, schema-driven settings,
  wallet, WiFi management, and device list.

- **Test the captive portal** — connect a phone to the router's WiFi,
  open any website, and the net4sats payment portal should appear.
  Choose a package, pay via Lightning or Cashu, and get internet.

---

## Documentation

| Document | Description |
|----------|-------------|
| [Setup guide](https://github.com/net4sats/net4sats.github.io/blob/docs/operator-guides/docs/setup-with-wizard.md) | Full walkthrough: prerequisites, scan, configure, deploy, verify, troubleshoot |
| [Admin panel guide](https://github.com/net4sats/net4sats.github.io/blob/docs/operator-guides/docs/admin-panel-guide.md) | configurationwizzard admin UI: dashboard, settings, wallet, WiFi, devices |
| [GL-MT6000 runbook](./docs/endo-runbook.md) | Device-specific setup guide for the GL-MT6000 (Flint 2) |
| [Architecture (AGENTS.md)](./AGENTS.md) | How net4sats repos fit together, boundary rules, identity derivation |

---

## Build from source

Requires Go 1.22+:

```sh
git clone https://github.com/net4sats/net4sats-wizard-go.git
cd net4sats-wizard-go
go build -o net4sats-wizard .
./net4sats-wizard
```

### Cross-compile all platforms

```sh
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -ldflags="-s -w" -o net4sats-wizard-linux-amd64 .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o net4sats-wizard-windows-amd64.exe .
CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -ldflags="-s -w" -o net4sats-wizard-darwin-amd64 .
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -ldflags="-s -w" -o net4sats-wizard-darwin-arm64 .
```

---

## How it works

```
Your laptop (wizard at :8099)
  │
  │  1. ARP scan + TCP probe → discover routers
  │  2. SSH connect → verify OpenWrt
  │  3. Install net4sats package
  │  4. Configure UCI (nodogsplash, wireless, network)
  │  5. Write Lightning address to config.json
  │  6. Restart services
  │
  ▼
Router (OpenWrt)
  ├── tollgate-wrt (:2121)      → payment backend
  ├── nodogsplash (:2050)       → captive portal redirect
  ├── configurationwizzard (:80) → admin dashboard SPA
  └── net4sats WiFi AP           → guest-facing network
```

The wizard is a **thin deployer**. All payment logic, identity derivation,
and Cashu/Lightning processing lives in
[tollgate-module-basic-go](https://github.com/net4sats/tollgate-module-basic-go)
on the router. The wizard only discovers, SSHes, installs packages, and
writes config — it does not handle payments or crypto.

See [AGENTS.md](./AGENTS.md) for the full architecture and boundary rules.

---

## Related repositories

| Repo | Role |
|------|------|
| [configurationwizzard](https://github.com/net4sats/configurationwizzard) | Router-side admin dashboard + captive portal (Preact SPA) |
| [tollgate-module-basic-go](https://github.com/net4sats/tollgate-module-basic-go) | Payment backend — Cashu + Lightning, runs on router |
| [net4sats-feed](https://github.com/net4sats/net4sats-feed) | OpenWrt package feed for net4sats |
| [net4sats.github.io](https://github.com/net4sats/net4sats.github.io) | Documentation and operator guides |

---

## Troubleshooting

**Router not found in scan:** Ensure it's powered on, running OpenWrt, and
on the same subnet. Try an ethernet cable directly to the LAN port.

**"Router is not running OpenWrt":** The wizard detected the router but
it's on stock firmware. Flash OpenWrt first — see the
[runbooks](https://github.com/net4sats/net4sats.github.io/tree/docs/operator-guides/docs).

**Deploy fails at install step:** The router needs internet to download
packages. If using WAN mode, plug ethernet into the WAN port. If using
WiFi repeater mode, verify the upstream WiFi credentials.

**Health check fails:** SSH in and check:
```sh
/etc/init.d/tollgate-wrt status
curl http://127.0.0.1:2121/
logread | grep tollgate
```

---

## License

MIT
