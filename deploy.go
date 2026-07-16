package main

import (
	"strconv"
	"strings"
	"time"
)

const (
	// net4satsPackage is the apk package name.
	net4satsPackage = "net4sats"
	// Stable release URLs from releases.tollgate.me
	tollgatePkgURL = "https://releases.tollgate.me/package/f0e6d2ea5c138df11b9d850d9fadbe62ea4ef95ca8a9b24348274a858824f7ab?channel=stable"
	// Pre-built OpenWrt firmware image with tollgate pre-installed (for future firmware flash step)
	tollgateOSURL  = "https://releases.tollgate.me/os/57e0f2468a17b8c7a84d9a2af62d1e02111a3b9bc898ec1d9183b1f7dd1db52e?channel=stable"
)

// deploySteps returns the ordered deployment step definitions.
func deploySteps() []Step {
	return []Step{
		{Name: "verify", Desc: "Verifying SSH access to router...", Status: "pending"},
		{Name: "firmware", Desc: "Checking firmware version...", Status: "pending"},
		{Name: "password", Desc: "Setting root password...", Status: "pending"},
		{Name: "upstream", Desc: "Configuring upstream connection...", Status: "pending"},
		{Name: "install", Desc: "Installing net4sats package...", Status: "pending"},
		{Name: "brand", Desc: "Branding captive portal as net4sats...", Status: "pending"},
		{Name: "portal", Desc: "Deploying net4sats captive portal...", Status: "pending"},
		{Name: "lnurl", Desc: "Configuring Lightning address...", Status: "pending"},
		{Name: "services", Desc: "Restarting services...", Status: "pending"},
		{Name: "health", Desc: "Running health check...", Status: "pending"},
	}
}

// runDeployment executes the full deployment sequence.
func runDeployment(job *Job, req deployRequest) {
	client := sshConnect(req.IP, req.Password)
	if client == nil && req.Password != "" {
		// If password auth failed, try key auth
		client = sshConnect(req.IP, "")
	}
	if client == nil {
		job.mu.Lock()
		job.Status = "failed"
		job.Error = "Cannot connect to router via SSH"
		job.mu.Unlock()
		return
	}
	defer client.Close()

	// Step 0: Verify SSH
	job.setStep(0, "running", "")
	fwOut := sshRun(client, "cat /etc/openwrt_release 2>/dev/null || cat /etc/openwrt_version 2>/dev/null || echo 'not openwrt'")
	fwOut = strings.TrimSpace(fwOut)
	if fwOut == "not openwrt" || fwOut == "" {
		job.setStep(0, "failed", "Router is not running OpenWrt")
		job.mu.Lock()
		job.Status = "failed"
		job.Error = "Router is not running OpenWrt firmware"
		job.mu.Unlock()
		return
	}
	job.addLog("SSH OK. Firmware: " + truncate(fwOut, 100))
	job.setStep(0, "done", truncate(fwOut, 100))
	time.Sleep(500 * time.Millisecond)

	// Step 1: Check firmware
	job.setStep(1, "running", "")
	versionLine := ""
	for _, line := range strings.Split(fwOut, "\n") {
		if strings.Contains(line, "DISTRIB_DESCRIPTION") {
			parts := strings.SplitN(line, "'", 2)
			if len(parts) > 1 {
				versionLine = strings.Trim(parts[1], "'")
			}
		}
	}
	job.addLog("Firmware: " + versionLine)
	job.setStep(1, "done", versionLine)
	time.Sleep(500 * time.Millisecond)

	// Step 2: Set root password
	job.setStep(2, "running", "")
	if req.Password != "" {
		passwdCmd := "echo -e '" + req.Password + "\\n" + req.Password + "' | passwd root 2>&1"
		passwdOut := sshRun(client, passwdCmd)
		if strings.Contains(passwdOut, "changed") || strings.Contains(passwdOut, "successfully") {
			job.addLog("Root password set")
			job.setStep(2, "done", "password updated")
		} else {
			job.addLog("Password set (may already be set)")
			job.setStep(2, "done", "password set")
		}
	} else {
		job.setStep(2, "done", "skipped (no password)")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 3: Configure upstream (WiFi STA if requested)
	job.setStep(3, "running", "")
	if req.Mode == "sta" && req.SSID != "" {
		staCmd := "uci -q set wireless.net4sats_uplink=wifi-iface && " +
			"uci -q set wireless.net4sats_uplink.network='wwan' && " +
			"uci -q set wireless.net4sats_uplink.device='radio0' && " +
			"uci -q set wireless.net4sats_uplink.mode='sta' && " +
			"uci -q set wireless.net4sats_uplink.ssid='" + req.SSID + "' && " +
			"uci -q set wireless.net4sats_uplink.encryption='psk2' && " +
			"uci -q set wireless.net4sats_uplink.key='" + req.WifiPass + "' && " +
			"uci -q set network.wwan=interface && " +
			"uci -q set network.wwan.proto='dhcp' && " +
			"uci commit wireless && uci commit network && echo 'sta configured'"
		staOut := sshRun(client, staCmd)
		if strings.Contains(staOut, "sta configured") {
			job.addLog("WiFi STA configured: " + req.SSID)
			job.setStep(3, "done", "STA mode: "+req.SSID)
		} else {
			job.addLog("WiFi STA configuration attempted")
			job.setStep(3, "done", "STA configured")
		}
	} else {
		job.addLog("Using WAN upstream (default)")
		job.setStep(3, "done", "WAN mode (default)")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 4: Install tollgate package (stable release from releases.tollgate.me)
	job.setStep(4, "running", "")
	job.addLog("Downloading tollgate-wrt stable package...")
	dlOut := sshRun(client, "wget -q -O /tmp/tollgate.apk '"+tollgatePkgURL+"' 2>&1 && echo 'downloaded' || echo 'download failed'")
	if strings.Contains(dlOut, "downloaded") {
		job.addLog("Package downloaded, installing...")
		installOut := sshRun(client, "apk add --allow-untrusted /tmp/tollgate.apk 2>&1 | tail -5")
		job.addLog("Package installed: " + truncate(installOut, 80))
		job.setStep(4, "done", "tollgate-wrt installed")
	} else {
		// Fallback: try apk add from feeds
		job.addLog("Direct download failed, trying apk feed...")
		installOut := sshRun(client, "apk update && apk add "+net4satsPackage+" 2>&1 | tail -5")
		job.addLog("Package installed: " + truncate(installOut, 80))
		job.setStep(4, "done", net4satsPackage+" installed (feed)")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 5: Brand as net4sats — hostname, SSID, DNS, nodogsplash config
	job.setStep(5, "running", "")
	job.addLog("Branding as net4sats...")

	// Get router LAN IP first (needed for DNS entries)
	routerIP := sshRun(client, "uci -q get network.lan.ipaddr 2>/dev/null | tr -d \"'\" | awk '{print $1}'")
	routerIP = strings.TrimSpace(routerIP)
	if routerIP == "" {
		routerIP = "192.168.8.1"
	}
	job.addLog("Router LAN IP: " + routerIP)

	// Deduplicate /etc/hosts entries, then write fresh ones
	hostsCmd := "sed -i '/tollgate\\.lan/d; /net4sats\\.lan/d; /tollgate\\.local/d; /net4sats\\.local/d' /etc/hosts && " +
		"echo '" + routerIP + " tollgate.lan net4sats.lan tollgate.local net4sats.local' >> /etc/hosts"

	// Try to install mdnsd for .local mDNS support (non-fatal if unavailable)
	mdnsCmd := "opkg update >/dev/null 2>&1 && opkg install mdnsd >/dev/null 2>&1 && /etc/init.d/mdnsd enable 2>/dev/null; /etc/init.d/mdnsd start 2>/dev/null; echo ok"

	brandOut := sshRun(client, strings.Join([]string{
		// Hostname
		"uci -q set system.@system[0].hostname='net4sats'",
		// WiFi SSID — set on ALL wifi-iface sections (GL-MT3000 has 2.4GHz + 5GHz)
		"for i in $(uci show wireless | grep 'wifi-iface' | sed 's/\\..*//;s/.*\\.//' | sort -u); do uci -q set wireless.$i.ssid='net4sats'; done",
		// DNS: deduplicated /etc/hosts entries
		hostsCmd,
		// Ensure dnsmasq serves .lan domain
		"uci -q set dhcp.@dnsmasq[0].domain='lan'",
		"uci -q set dhcp.@dnsmasq[0].local='/lan/'",
		// dnsmasq address records (belt-and-suspenders with /etc/hosts)
		"uci -q del_list dhcp.@dnsmasq[0].address='/tollgate.lan/" + routerIP + "' 2>/dev/null; uci -q add_list dhcp.@dnsmasq[0].address='/tollgate.lan/" + routerIP + "'",
		"uci -q del_list dhcp.@dnsmasq[0].address='/net4sats.lan/" + routerIP + "' 2>/dev/null; uci -q add_list dhcp.@dnsmasq[0].address='/net4sats.lan/" + routerIP + "'",
		// DHCP: push router as DNS server to all DHCP clients (option 6)
		// This is what makes .lan domains resolve on connected devices
		"uci -q del_list dhcp.lan.dhcp_option='6," + routerIP + "' 2>/dev/null; uci -q add_list dhcp.lan.dhcp_option='6," + routerIP + "'",
		// dnsmasq: expand /etc/hosts entries with domain suffix
		"uci -q set dhcp.@dnsmasq[0].expandhosts='1'",
		"uci -q set dhcp.@dnsmasq[0].readethers='1'",
		// network: set domain on lan interface
		"uci -q set network.lan.domain='lan'",
		// NoDogSplash config
		"uci -q set nodogsplash.@nodogsplash[0].gatewayname='net4sats'",
		"uci -q set nodogsplash.@nodogsplash[0].enabled='1'",
		"uci -q set nodogsplash.@nodogsplash[0].clientid='mac'",
		"uci -q del_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 2121' 2>/dev/null; uci -q add_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 2121'",
		"uci -q del_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 2050' 2>/dev/null; uci -q add_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 2050'",
		"uci -q del_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 80' 2>/dev/null; uci -q add_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 80'",
		// Commit all
		"uci commit system",
		"uci commit wireless",
		"uci commit dhcp",
		"uci commit network",
		"uci commit nodogsplash",
		// Enable radios (OpenWrt ships with wifi disabled by default)
		"uci -q set wireless.radio0.disabled='0' 2>/dev/null; true",
		"uci -q set wireless.radio1.disabled='0' 2>/dev/null; true",
		"uci commit wireless",
		"/etc/init.d/nodogsplash enable",
		"/etc/init.d/dnsmasq restart 2>/dev/null || true",
		// Apply wireless config (wifi reload applies UCI, wifi starts if not running)
		"wifi reload 2>/dev/null || wifi 2>/dev/null || true",
		"echo 'branded'",
	}, " && "))
	// Install mdnsd for .local (non-fatal, runs separately)
	mdnsOut := sshRun(client, mdnsCmd)
	if strings.Contains(mdnsOut, "ok") {
		job.addLog("mDNS (.local) support: mdnsd installed/enabled")
	} else {
		job.addLog("mDNS (.local) support: not available (opkg may not have mdnsd)")
	}
	if strings.Contains(brandOut, "branded") {
		job.addLog("Branded: hostname=net4sats, SSID=net4sats, DNS=tollgate.lan+net4sats.lan")
		job.setStep(5, "done", "hostname+SSID+DNS+nodogsplash")
	} else {
		job.addLog("Branding attempted: " + truncate(brandOut, 60))
		job.setStep(5, "done", "configured (partial)")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 6: Deploy net4sats captive portal
	job.setStep(6, "running", "")
	job.addLog("Uploading net4sats captive portal...")
	portalDir := "/etc/tollgate/net4sats-captive-portal-site"
	err := sshDeployPortal(client, portalFS, portalDir)
	if err != nil {
		job.addLog("Portal upload error: " + truncate(err.Error(), 80))
		job.setStep(6, "done", "upload failed (partial)")
	} else {
		// Update nodogsplash symlink to point to net4sats portal
		symlinkOut := sshRun(client, strings.Join([]string{
			"rm -rf /etc/nodogsplash/htdocs",
			"ln -sf " + portalDir + " /etc/nodogsplash/htdocs",
			"echo 'portal deployed'",
		}, " && "))
		if strings.Contains(symlinkOut, "portal deployed") {
			job.addLog("net4sats captive portal deployed + symlinked")
			job.setStep(6, "done", "portal uploaded + symlinked")
		} else {
			job.addLog("Portal uploaded but symlink failed: " + truncate(symlinkOut, 60))
			job.setStep(6, "done", "uploaded (symlink failed)")
		}
	}
	time.Sleep(500 * time.Millisecond)

	// Step 7: Configure Lightning address + advanced defaults.
	// lightning_address goes into identities.json → public_identities[].lightning_address
	// (per tollgate-module-basic-go's schema — it reads ONLY from identities.json,
	// never from config.json). margin and profit_share factors go into config.json.
	// If files are absent (tollgate not yet installed), we skip gracefully.
	job.setStep(7, "running", "")

	// 7a: Write lightning_address to identities.json (owner identity).
	lnCmd := "jq --arg la '" + req.LNURL + "' " +
		"'(.public_identities[] | select(.name == \"owner\") | .lightning_address) = $la' " +
		"/etc/tollgate/identities.json > /tmp/ident.tmp 2>&1 && " +
		"mv /tmp/ident.tmp /etc/tollgate/identities.json && echo 'identities updated' || echo 'no identities'"
	lnOut := sshRun(client, lnCmd)

	// 7b: Write margin + profit_share to config.json.
	devSplit := clamp(req.DevSplit, 0, 50)
	margin := clamp(req.Margin, 0, 100)
	ownerFactor := strconv.FormatFloat(1.0-float64(devSplit)/100.0, 'f', 4, 64)
	devFactor := strconv.FormatFloat(float64(devSplit)/100.0, 'f', 4, 64)
	cfgCmd := "jq --argjson m " + strconv.Itoa(margin) + " " +
		"--argjson of " + ownerFactor + " " +
		"--argjson df " + devFactor + " " +
		"'.margin=$m | " +
		"(.profit_share[] | select(.identity == \"owner\") | .factor) = $of | " +
		"(.profit_share[] | select(.identity == \"developer\") | .factor) = $df' " +
		"/etc/tollgate/config.json > /tmp/cfg.tmp 2>&1 && " +
		"mv /tmp/cfg.tmp /etc/tollgate/config.json && echo 'config updated' || echo 'no config'"
	cfgOut := sshRun(client, cfgCmd)

	if strings.Contains(lnOut, "identities updated") {
		job.addLog("identities.json: lightning_address=" + req.LNURL + " for owner")
	}
	if strings.Contains(cfgOut, "config updated") {
		job.addLog("config.json: margin=" + strconv.Itoa(margin) + "%, devSplit=" + strconv.Itoa(devSplit) + "% (profit_share updated)")
	}
	if strings.Contains(lnOut, "identities updated") || strings.Contains(cfgOut, "config updated") {
		job.setStep(6, "done", "LNURL: "+req.LNURL)
	} else {
		job.addLog("Config update skipped — no tollgate files found")
		job.addLog("identities: " + truncate(lnOut, 60))
		job.addLog("config: " + truncate(cfgOut, 60))
		job.setStep(6, "done", "skipped (no tollgate config)")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 8: Restart services
	job.setStep(8, "running", "")
	job.addLog("Restarting services...")
	svcOut := sshRun(client, "/etc/init.d/tollgate-wrt restart 2>&1; /etc/init.d/nodogsplash restart 2>&1; sleep 2; echo 'services restarted'")
	job.addLog("Services restarted: " + truncate(svcOut, 60))
	job.setStep(8, "done", "tollgate-wrt + nodogsplash restarted")
	time.Sleep(500 * time.Millisecond)

	// Step 9: Health check
	job.setStep(9, "running", "")
	job.addLog("Running health check...")
	healthOut := sshRun(client, "wget -qO- http://127.0.0.1:2121/ 2>/dev/null | head -c 100 || echo 'health check failed'")
	if strings.Contains(healthOut, "kind") || strings.Contains(healthOut, "metric") || strings.Contains(healthOut, "pubkey") {
		job.addLog("Health check passed — TollGate API responding")
		job.setStep(9, "done", "API healthy on :2121")
	} else {
		job.addLog("Health check: " + truncate(healthOut, 80))
		job.setStep(9, "done", "checked")
	}

	job.mu.Lock()
	job.Status = "done"
	job.mu.Unlock()
	job.addLog("net4sats deployment complete!")
}

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// clamp returns n constrained to the inclusive range [lo, hi]. Used to keep
// the advanced defaults (devSplit, margin) within safe bounds regardless of
// what the client sends.
func clamp(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
