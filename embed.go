package main

import "embed"

//go:embed index.html
var indexHTML []byte

//go:embed all:portal
var portalFS embed.FS

//go:embed all:admin
var adminFS embed.FS

//go:embed rpcd/tollgate
var rpcdTollgate []byte

//go:embed rpcd/tollgate_acl.json
var rpcdACL []byte

//go:embed uhttpd_net4sats
var uhttpdNet4sats []byte
