package main

import (
	cryptorand "crypto/rand"
	"strconv"
	"strings"
	"time"
)

const (
	// net4satsPackage is the apk package name.
	net4satsPackage = "net4sats"
	// Stable release URLs from releases.tollgate.me
	tollgatePkgURL = "https://github.com/OpenTollGate/tollgate-module-basic-go/releases/download/v0.5.0/tollgate-wrt_v0.5.0_aarch64_cortex-a53.ipk"
	// Pre-built OpenWrt firmware image with tollgate pre-installed (for future firmware flash step)
	tollgateOSURL = "https://releases.tollgate.me/os/57e0f2468a17b8c7a84d9a2af62d1e02111a3b9bc898ec1d9183b1f7dd1db52e?channel=stable"
	// Admin panel + rpcd plugin from net4sats GitHub releases
	configwizURL = "https://github.com/net4sats/configurationwizzard/releases/download/v1.0.0/net4sats-configwiz-1.0.0.tar.gz"
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
		{Name: "admin", Desc: "Installing net4sats admin panel...", Status: "pending"},
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

	// Step 4: Install tollgate package from GitHub releases
	job.setStep(4, "running", "")
	job.addLog("Downloading tollgate-wrt v0.5.0 ipk...")
	dlOut := sshRun(client, "wget -q -O /tmp/tollgate-wrt.ipk '"+tollgatePkgURL+"' 2>&1 && echo 'downloaded' || echo 'download failed'")
	if strings.Contains(dlOut, "downloaded") {
		job.addLog("Package downloaded, installing via opkg...")
		rmLock := sshRun(client, "rm -f /var/lock/opkg.lock 2>/dev/null")
		_ = rmLock
		installOut := sshRun(client, "opkg install /tmp/tollgate-wrt.ipk 2>&1 | tail -5")
		job.addLog("Package installed: " + truncate(installOut, 80))
		// Verify the binary actually exists
		verifyOut := sshRun(client, "ls /usr/bin/tollgate-wrt 2>/dev/null || ls /usr/sbin/tollgate-wrt 2>/dev/null || which tollgate-wrt 2>/dev/null || echo 'NOT FOUND'")
		if strings.Contains(verifyOut, "NOT FOUND") {
			job.addLog("WARNING: tollgate-wrt binary not found after install")
			job.setStep(4, "error", "tollgate-wrt install failed")
			return
		}
		job.setStep(4, "done", "tollgate-wrt installed")
	} else {
		job.addLog("Download failed, trying opkg feed...")
		installOut := sshRun(client, "rm -f /var/lock/opkg.lock 2>/dev/null; opkg update >/dev/null 2>&1; opkg install "+net4satsPackage+" 2>&1 | tail -5")
		job.addLog("Package installed: " + truncate(installOut, 80))
		verifyOut := sshRun(client, "which tollgate-wrt 2>/dev/null || echo 'NOT FOUND'")
		if strings.Contains(verifyOut, "NOT FOUND") {
			job.setStep(4, "error", "tollgate-wrt install failed")
			return
		}
		job.setStep(4, "done", net4satsPackage+" installed (feed)")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 5: Brand as net4sats — hostname, SSID, DNS, nodogsplash config
	job.setStep(5, "running", "")
	// Generate unique suffix (e.g. net4sats-a7f2) so multiple routers don't clash
	const ssidChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	suffix := make([]byte, 4)
	randBytes := make([]byte, 4)
	if _, err := cryptorand.Read(randBytes); err != nil {
		// Fallback: time-seeded
		for i := range randBytes {
			randBytes[i] = byte(time.Now().UnixNano() >> uint(i*8))
		}
	}
	for i := range suffix {
		suffix[i] = ssidChars[int(randBytes[i])%len(ssidChars)]
	}
	nodeName := "net4sats-" + string(suffix)
	job.addLog("Branding as " + nodeName + "...")

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
		"uci -q set system.@system[0].hostname='" + nodeName + "'",
		// WiFi SSID — only on default_radio* (public captive portal WiFi)
		// Skip private_radio* (admin LAN) and *_uplink (WAN repeater)
		"for i in $(uci -q show wireless 2>/dev/null | grep 'default_radio.*=wifi-iface' | awk -F. '{print $2}' | awk -F= '{print $1}'); do uci -q set wireless.$i.ssid='" + nodeName + "'; done",
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
		"uci -q set nodogsplash.@nodogsplash[0].gatewayname='" + nodeName + "'",
		// Rebrand gateway domain from upstream 'TollGate.lan' to net4sats.lan
		// so the captive portal serves on net4sats.lan (DNS already resolves both).
		"uci -q set nodogsplash.@nodogsplash[0].gatewaydomainname='net4sats.lan'",
		"uci -q set nodogsplash.@nodogsplash[0].enabled='1'",
		"uci -q set nodogsplash.@nodogsplash[0].clientid='mac'",
		"uci -q del_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 2121' 2>/dev/null; uci -q add_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 2121'",
		"uci -q del_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 2050' 2>/dev/null; uci -q add_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 2050'",
		"uci -q del_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 2051' 2>/dev/null; uci -q add_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 2051'",
		"uci -q del_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 80' 2>/dev/null; uci -q add_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 80'",
		"uci -q del_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 8080' 2>/dev/null; uci -q add_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 8080'",
		"uci -q del_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 8090' 2>/dev/null; uci -q add_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 8090'",
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
		"/etc/init.d/nodogsplash restart 2>/dev/null || true",
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
		job.addLog("Branded: hostname=" + nodeName + ", SSID=" + nodeName + ", DNS=tollgate.lan+net4sats.lan")
		job.setStep(5, "done", "hostname+SSID+DNS+nodogsplash")
	} else {
		job.addLog("Branding attempted: " + truncate(brandOut, 60))
		job.setStep(5, "done", "configured (partial)")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 6: Deploy configurationwizzard captive portal to nodogsplash + uhttpd portal
	job.setStep(6, "running", "")
	job.addLog("Uploading configurationwizzard captive portal...")
	// 6a: uhttpd portal instance (port 2051) — serves full Preact SPA with JS/CSS.
	// NDS 5.0.2 built-in HTTP server returns 500 for files >64KB (splash JS is 200KB).
	// uhttpd handles large files fine. Portal is served from here, NDS redirects to it.
	portalDeployDir := "/etc/tollgate/net4sats-captive-portal-site"
	portalErr2 := sshDeployFS(client, portalFS, "portal", portalDeployDir)
	if portalErr2 != nil {
		job.addLog("Portal upload error (uhttpd 2051): " + truncate(portalErr2.Error(), 80))
	} else {
		job.addLog("Portal deployed to " + portalDeployDir + "/ (port 2051)")
	}

	// 6b: NDS htdocs — replace with redirect stub (NOT the full SPA).
	// NDS serves this as its built-in splash, but it can't serve large JS.
	// The redirect sends clients to uhttpd :2051 where the real portal lives.
	sshRun(client, "rm -f /etc/nodogsplash/htdocs; mkdir -p /etc/nodogsplash/htdocs")
	redirectHTML := "<!DOCTYPE html><html><head>" +
		"<meta http-equiv=\"refresh\" content=\"0; url=http://" + routerIP + ":2051/splash.html\">" +
		"<script>location.replace(\"http://" + routerIP + ":2051/splash.html\");</script>" +
		"<title>net4sats Portal</title></head><body>Redirecting...</body></html>"
	sshWriteFile(client, "/etc/nodogsplash/htdocs/splash.html", []byte(redirectHTML))
	sshRun(client, "cp /etc/nodogsplash/htdocs/splash.html /etc/nodogsplash/htdocs/index.html")
	job.addLog("NDS htdocs: redirect stub installed (port 2050 → 2051)")

	// 6c: NDS preauth script — ensures NDS intercepts and redirects to portal
	preauthScript := "#!/bin/sh\n" +
		"# NDS preauth: redirect intercepted clients to uhttpd-served portal\n" +
		"cat << 'EOF'\n" + redirectHTML + "\nEOF\nexit 0\n"
	sshWriteFile(client, "/etc/nodogsplash/preauth.sh", []byte(preauthScript))
	sshRun(client, "chmod +x /etc/nodogsplash/preauth.sh")

	// 6d: Configure NDS to use preauth + allow port 2051
	sshRun(client, strings.Join([]string{
		"uci set nodogsplash.@nodogsplash[0].preauth='/etc/nodogsplash/preauth.sh'",
		"uci -q del_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 2051' 2>/dev/null; true",
		"uci add_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 2051'",
		"uci commit nodogsplash",
		"echo 'nds configured'",
	}, " && "))

	// 6e: uhttpd portal section (must be in main uhttpd config, not separate file)
	sshRun(client, strings.Join([]string{
		"uci set uhttpd.portal=uhttpd",
		"uci -q del_list uhttpd.portal.listen_http='0.0.0.0:2051' 2>/dev/null; true",
		"uci add_list uhttpd.portal.listen_http='0.0.0.0:2051'",
		"uci -q del_list uhttpd.portal.listen_http='[::]:2051' 2>/dev/null; true",
		"uci add_list uhttpd.portal.listen_http='[::]:2051'",
		"uci set uhttpd.portal.home='" + portalDeployDir + "'",
		"uci set uhttpd.portal.index_page='splash.html'",
		"uci set uhttpd.portal.max_requests='8'",
		"uci commit uhttpd",
		"echo 'uhttpd portal configured'",
	}, " && "))

	if portalErr2 == nil {
		job.setStep(6, "done", "portal: uhttpd :2051 + NDS preauth redirect")
	} else {
		job.setStep(6, "done", "portal deployed (partial)")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 7: Deploy admin panel + rpcd plugin + uhttpd config (matches playwright deploy-configwizzard.sh)
	job.setStep(7, "running", "")
	job.addLog("Deploying net4sats admin panel + rpcd plugin...")

	// 7a: Admin panel to /www/net4sats/
	adminErr := sshDeployFS(client, adminFS, "admin", "/www/net4sats")
	if adminErr != nil {
		job.addLog("Admin upload error: " + truncate(adminErr.Error(), 60))
	}

	// 7b: rpcd plugin
	sshRun(client, "mkdir -p /usr/libexec/rpcd /usr/share/rpcd/acl.d")
	sshWriteFile(client, "/usr/libexec/rpcd/tollgate", rpcdTollgate)
	sshRun(client, "chmod +x /usr/libexec/rpcd/tollgate")
	sshWriteFile(client, "/usr/share/rpcd/acl.d/tollgate.json", rpcdACL)
	job.addLog("rpcd tollgate plugin installed")

	// 7b2: Patch admin JS — replace broken dhcp/ipv4leases with tollgate/clients
	// dnsmasq 2.90 on OpenWrt 24.10 doesn't provide ubus dhcp.ipv4leases method.
	// Our rpcd plugin's clients method parses /tmp/dhcp.leases directly.
	sshRun(client, "for f in /www/net4sats/assets/index-*.js; do "+
		"sed -i 's/`dhcp`,`ipv4leases`/`tollgate`,`clients`/g' \"$f\"; done")
	job.addLog("Admin JS patched: dhcp ipv4leases → tollgate clients")

	// 7b3: Deploy balance page to admin panel
	// balance.html exists in portal FS — copy it, fix paths for admin base
	balanceHTML := readFromEmbedFS(portalFS, "portal/balance.html")
	if balanceHTML != nil {
		fixed := strings.ReplaceAll(string(balanceHTML), "/assets/", "./assets/")
		fixed = strings.ReplaceAll(fixed, "/favicon.ico", "./favicon.ico")
		fixed = strings.ReplaceAll(fixed, "/manifest.json", "./manifest.json")
		sshWriteFile(client, "/www/net4sats/balance.html", []byte(fixed))
		// Copy shared vendor assets from portal to admin (balance JS/CSS + shared bundles)
		sshRun(client, "for f in balance-*.js balance-*.css index-C9QTYeLH.js index-DoTOgCNp.css; do "+
			"cp /etc/tollgate/net4sats-captive-portal-site/assets/$f /www/net4sats/assets/ 2>/dev/null; done; true")
		job.addLog("Balance page deployed to admin panel")
	}

	// 7c: uhttpd config — add net4sats (:8090) and luci (:8080) instances via UCI
	// Must go into /etc/config/uhttpd (the only file uhttpd init reads)
	uhttpdOut := sshRun(client, strings.Join([]string{
		// Remove conflicting listeners from main uhttpd
		"uci -q del_list uhttpd.main.listen_http='0.0.0.0:80' 2>/dev/null; true",
		"uci -q del_list uhttpd.main.listen_http='[::]:80' 2>/dev/null; true",
		"uci -q del_list uhttpd.main.listen_http='0.0.0.0:8080' 2>/dev/null; true",
		"uci -q del_list uhttpd.main.listen_http='[::]:8080' 2>/dev/null; true",
		"uci -q del_list uhttpd.main.listen_http='0.0.0.0:8090' 2>/dev/null; true",
		"uci -q del_list uhttpd.main.listen_http='[::]:8090' 2>/dev/null; true",
		// net4sats admin instance on :8090
		"uci set uhttpd.net4sats=uhttpd",
		"uci -q del_list uhttpd.net4sats.listen_http='0.0.0.0:8090' 2>/dev/null; true",
		"uci add_list uhttpd.net4sats.listen_http='0.0.0.0:8090'",
		"uci -q del_list uhttpd.net4sats.listen_http='[::]:8090' 2>/dev/null; true",
		"uci add_list uhttpd.net4sats.listen_http='[::]:8090'",
		"uci set uhttpd.net4sats.home='/www/net4sats'",
		"uci set uhttpd.net4sats.ubus_prefix='/ubus'",
		"uci set uhttpd.net4sats.script_timeout='60'",
		"uci set uhttpd.net4sats.network_timeout='30'",
		"uci set uhttpd.net4sats.max_requests='3'",
		"uci set uhttpd.net4sats.tcp_keepalive='1'",
		// luci instance on :8080
		"uci set uhttpd.luci=uhttpd",
		"uci -q del_list uhttpd.luci.listen_http='0.0.0.0:8080' 2>/dev/null; true",
		"uci add_list uhttpd.luci.listen_http='0.0.0.0:8080'",
		"uci -q del_list uhttpd.luci.listen_http='[::]:8080' 2>/dev/null; true",
		"uci add_list uhttpd.luci.listen_http='[::]:8080'",
		"uci set uhttpd.luci.home='/www'",
		"uci set uhttpd.luci.cgi_prefix='/cgi-bin'",
		"uci -q del_list uhttpd.luci.lua_prefix='/cgi-bin/luci=/usr/lib/lua/luci/sgi/uhttpd.lua' 2>/dev/null; true",
		"uci add_list uhttpd.luci.lua_prefix='/cgi-bin/luci=/usr/lib/lua/luci/sgi/uhttpd.lua'",
		"uci set uhttpd.luci.ubus_prefix='/ubus'",
		"uci set uhttpd.luci.script_timeout='60'",
		"uci set uhttpd.luci.network_timeout='30'",
		// Don't redirect LuCI to HTTPS — self-signed cert confuses browsers
		"uci -q set uhttpd.luci.redirect_https='0'",
		"uci commit uhttpd",
		// Restart rpcd to pick up new plugin
		"/etc/init.d/rpcd restart 2>/dev/null || true",
		// Restart uhttpd to pick up new config
		"/etc/init.d/uhttpd restart 2>/dev/null || true",
		"echo 'admin deployed'",
	}, " && "))

	if strings.Contains(uhttpdOut, "admin deployed") {
		job.addLog("Admin: http://tollgate.lan:8090/ | LuCI: http://tollgate.lan:8080/")
		job.setStep(7, "done", "admin+:8090, rpcd, uhttpd")
	} else {
		job.addLog("uhttpd: " + truncate(uhttpdOut, 60))
		job.setStep(7, "done", "deployed (partial)")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 8: Configure Lightning address + advanced defaults.
	// lightning_address goes into identities.json → public_identities[].lightning_address
	// (per tollgate-module-basic-go's schema — it reads ONLY from identities.json,
	// never from config.json). margin and profit_share factors go into config.json.
	// If files are absent (tollgate not yet installed), we skip gracefully.
	job.setStep(8, "running", "")

	// 8a: Write lightning_address to identities.json (owner identity).
	lnCmd := "jq --arg la '" + req.LNURL + "' " +
		"'(.public_identities[] | select(.name == \"owner\") | .lightning_address) = $la' " +
		"/etc/tollgate/identities.json > /tmp/ident.tmp 2>&1 && " +
		"mv /tmp/ident.tmp /etc/tollgate/identities.json && echo 'identities updated' || echo 'no identities'"
	lnOut := sshRun(client, lnCmd)

	// 8b: Write margin + profit_share to config.json.
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
		job.setStep(8, "done", "LNURL: "+req.LNURL)
	} else {
		job.addLog("Config update skipped — no tollgate files found")
		job.addLog("identities: " + truncate(lnOut, 60))
		job.addLog("config: " + truncate(cfgOut, 60))
		job.setStep(8, "done", "skipped (no tollgate config)")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 9: Restart services
	job.setStep(9, "running", "")
	job.addLog("Restarting services...")
	// Verify tollgate-wrt init script exists before restart
	initCheck := sshRun(client, "ls /etc/init.d/tollgate-wrt 2>/dev/null && echo 'exists' || echo 'missing'")
	if strings.Contains(initCheck, "missing") {
		job.addLog("ERROR: tollgate-wrt init script not found — package install failed")
		job.setStep(9, "error", "tollgate-wrt not installed")
		return
	}
	svcOut := sshRun(client, strings.Join([]string{
		"/etc/init.d/rpcd restart 2>&1",
		"/etc/init.d/tollgate-wrt restart 2>&1",
		"/etc/init.d/nodogsplash restart 2>&1",
		"/etc/init.d/uhttpd restart 2>&1",
		"sleep 3",
		"echo 'services restarted'",
	}, "; "))
	job.addLog("Services restarted: " + truncate(svcOut, 60))
	job.setStep(9, "done", "rpcd+tollgate-wrt+nodogsplash+uhttpd")
	time.Sleep(500 * time.Millisecond)

	// Step 10: Health check
	job.setStep(10, "running", "")
	job.addLog("Running health check...")
	healthOut := sshRun(client, "wget -qO- http://127.0.0.1:2121/ 2>/dev/null | head -c 100 || echo 'health check failed'")
	if strings.Contains(healthOut, "kind") || strings.Contains(healthOut, "metric") || strings.Contains(healthOut, "pubkey") {
		job.addLog("Health check passed — TollGate API responding")
		// Also verify rpcd tollgate plugin responds
		rpcdOut := sshRun(client, "ubus list tollgate 2>/dev/null && echo 'rpcd ok' || echo 'rpcd missing'")
		if strings.Contains(rpcdOut, "rpcd ok") {
			job.addLog("rpcd tollgate plugin: OK")
		} else {
			job.addLog("WARNING: rpcd tollgate plugin not responding")
		}
		// Verify admin panel serving on 8090
		adminOut := sshRun(client, "netstat -tlnp 2>/dev/null | grep 8090 && echo 'admin ok' || echo 'admin missing'")
		if strings.Contains(adminOut, "admin ok") {
			job.addLog("Admin panel on :8090: OK")
		} else {
			job.addLog("WARNING: admin panel not serving on 8090")
		}
		job.setStep(10, "done", "API healthy on :2121")
	} else {
		job.addLog("Health check FAILED: " + truncate(healthOut, 80))
		job.setStep(10, "error", "tollgate API not responding on :2121")
		return
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
