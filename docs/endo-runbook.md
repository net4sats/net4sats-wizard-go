# net4sats — GL-MT6000 Setup Guide

## What you need

- A GL-MT6000 router (power cable, ethernet cable)
- A laptop with WiFi or ethernet
- A Lightning wallet (e.g. Wallet of Satoshi, Muun, Zeus) or a Cashu wallet

## Step 1: Download the wizard

Download the wizard for your laptop:

- **macOS (Intel)**: [net4sats-wizard-darwin-amd64](https://github.com/net4sats/net4sats-wizard-go/releases/latest)
- **macOS (Apple Silicon M1/M2/M3)**: [net4sats-wizard-darwin-arm64](https://github.com/net4sats/net4sats-wizard-go/releases/latest)
- **Windows**: [net4sats-wizard-windows-amd64.exe](https://github.com/net4sats/net4sats-wizard-go/releases/latest)
- **Linux**: [net4sats-wizard-linux-amd64](https://github.com/net4sats/net4sats-wizard-go/releases/latest)

## Step 2: Connect the router

1. Plug the GL-MT6000 into power.
2. Connect one end of the ethernet cable to your laptop.
3. Connect the other end to the router's **LAN port** (not WAN).
4. Wait 60 seconds for the router to boot.

The router will be at **192.168.1.1** by default.

## Step 3: Run the wizard

**macOS:** Open Terminal, navigate to the download, and run:
```bash
chmod +x net4sats-wizard-darwin-*
./net4sats-wizard-darwin-amd64    # Intel Mac
./net4sats-wizard-darwin-arm64    # Apple Silicon Mac
```

**Windows:** Double-click `net4sats-wizard-windows-amd64.exe`

**Linux:**
```bash
chmod +x net4sats-wizard-linux-amd64
./net4sats-wizard-linux-amd64
```

Your browser will open automatically at `http://localhost:8099`.

If it doesn't, open your browser and go to: **http://localhost:8099**

## Step 4: Select your router

The wizard scans your network automatically. You should see:

> **192.168.1.1 — GL-MT6000 — OpenWrt 25.12.0**

Click on it to select it.

If no router appears:
- Make sure you're connected via ethernet to the router's LAN port
- Try clicking "Scan Again"
- Check that the router has been powered on for at least 60 seconds

## Step 5: Set the admin password

Enter a password for the router's admin account. You'll need this to SSH into the router later if needed.

- Enter the password twice (they must match)
- **Write this down** — there is no password recovery without it

## Step 6: Configure upstream internet

Choose how the router connects to the internet:

**Option A — Ethernet WAN (recommended):**
Connect an ethernet cable from your wall/modem to the router's **WAN port**.
No configuration needed — just plug it in.

**Option B — WiFi uplink:**
If you want the router to connect to an existing WiFi network:
1. Select "WiFi Client"
2. Enter the SSID (network name) of your upstream WiFi
3. Enter the WiFi password

## Step 7: Enter your Lightning address

Enter the Lightning address where you want to receive payments. This is where the sats from people buying internet access will go.

Example: `you@walletofsatoshi.com`

The router supports multiple mints:
- coinos.io
- minibits.cash
- testnut.cashu.exchange

Price is set to **1 sat per 21 MB** by default.

## Step 8: Deploy

Click **"Deploy net4sats"**. The wizard will:
1. SSH into the router
2. Set the admin password
3. Configure upstream connectivity
4. Set your Lightning address
5. Restart all services

This takes about 30 seconds. You'll see a live progress log.

## Step 9: Success

When deployment completes, you'll see:
- The router's new IP address
- The WiFi SSID clients should connect to
- The admin panel URL

**Connect to the router's WiFi** and try opening a website. You should see the net4sats captive portal asking for payment.

## How people pay for internet

When someone connects to your router's WiFi:
1. Their browser shows the **net4sats portal**
2. They choose a payment method: **Lightning** or **Cashu**
3. They pay 1 sat per 21 MB of data
4. Internet access is granted instantly

The sats go to your Lightning address.

## Admin panel

Once deployed, you can access the admin panel at:
- **http://192.168.1.1:8080** (LuCI — OpenWrt admin)
- **http://net4sats.lan** (net4sats portal — if DNS is configured)

Default credentials: the password you set in Step 5.

## Troubleshooting

**Router not found by wizard:**
- Check ethernet cable is in LAN port, not WAN
- Try `ping 192.168.1.1` in a terminal
- Factory reset the router (hold reset button 10 seconds)

**Captive portal not showing:**
- Make sure you're connected to the router's WiFi, not your home WiFi
- Try opening `http://neverssl.com` in a browser (forces redirect)
- Clear browser cache

**Payments not working:**
- Verify your Lightning address is correct
- Check that the router has internet (WAN or WiFi uplink)
- Check tollgate status: SSH in and run `wget -qO- http://localhost:2121/`

**Can't reach admin panel:**
- Try both `http://192.168.1.1:8080` and `http://192.168.1.1`
- The router might have changed IP after deploy — check the wizard success screen

## Technical details

- Router: GL-MT6000 ( MediaTek MT7986A, 1GB RAM, aarch64)
- Firmware: OpenWrt 25.12.0
- Backend: TollGate v0.5.0-alpha3
- Portal: configurationwizzard (Preact PWA)
- Captive portal: nodogsplash 5.0.2
- Pricing: NIP-61 kind 10021 (1 sat per 21MB, 3 mints)

---

*This runbook was validated on a live GL-MT6000 running OpenWrt 25.12.0.*
