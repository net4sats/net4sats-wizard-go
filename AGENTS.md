# AGENTS.md — net4sats Architecture for LLM Sessions

## Architectural Principle

**net4sats repos are thin UI wrappers.** All business logic resides in
`tollgate-module-basic-go` (the Go backend installed as a system package
on OpenWrt routers). The net4sats repos only call backend APIs and render
results — they do NOT implement or duplicate business logic.

## Repo Map

### net4sats-wizard-go (this repo)
**Role:** Cross-platform onboarding wizard (Go binary, served locally).
- Discovers routers on LAN (ARP scan + TCP probe)
- Walks operator through router deployment
- Calls the Go backend's REST API on the router for identity, status, etc.
- Does NOT implement: DeriveIPv4, DeriveMAC, BIP39 mnemonic, kind:0 publishing
- Does NOT ship: uci-defaults scripts (those belong to tollgate-module-basic-go packaging)

### configurationwizzard
**Role:** Router-side admin dashboard (Preact PWA, served on the router).
- Served by uhttpd on the router (port 8090 for admin, port 80 for captive portal)
- Calls ubus/rpcd for OpenWrt config (WiFi, DHCP, settings)
- Calls the Go backend's REST API for identity, pricing, payments
- Does NOT implement: ubus calls are JSON-RPC passthrough, no backend logic

### tollgate-module-basic-go (OpenTollGate/tollgate-module-basic-go)
**Role:** All business logic. The core backend.
- Identity: `src/identity/` — DeriveIPv4, DeriveMAC, PrivateKeyToMnemonic, MnemonicToPrivateKey
- API: GET /identity, POST /identity/reveal-seed
- uci-defaults: `packaging/files/etc/uci-defaults/95-router-identity`
- Pricing: NIP-61 kind:10021 advertisement events (with supports_ln tags per PR #181)
- Payments: Cashu ecash + Lightning invoices
- Kind:0 profile publishing (opt-in)

## Identity Derivation (belongs ONLY in tollgate-module-basic-go)

```
merchant private key (identities.json)
  │
  ├─ SHA256(npub + "tollgate-lan-ipv4") → 10.X.Y.1/24 (DeriveIPv4)
  ├─ SHA256(npub + "tollgate-lan-mac") → 02:xx:xx:xx:xx:xx (DeriveMAC)
  └─ BIP39 mnemonic (24 words) → seed phrase (PrivateKeyToMnemonic)
```

All three derivation functions live in `tollgate-module-basic-go/src/identity/`.
Wizard and admin UI call `GET /identity` and `POST /identity/reveal-seed` to
get the results. Zero computation in the UI layer.

## Key Rules for Agents

1. **Do NOT add derivation logic to this repo.** If you need DeriveIPv4 or
   DeriveMAC or BIP39 conversion, add it to tollgate-module-basic-go and call
   the API from here.

2. **Do NOT ship uci-defaults scripts from this repo.** Those belong in
   tollgate-module-basic-go's packaging directory.

3. **Backward compatibility is paramount.** The wizard must work with older
   versions of tollgate-module-basic-go (v0.5.0-alpha3). New API calls should
   gracefully degrade if the endpoint doesn't exist yet.

4. **This repo is a thin wrapper.** If you find yourself writing more logic
   than API-call-and-render, stop and reconsider whether it belongs in the
   backend instead.
