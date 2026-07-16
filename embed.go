package main

import "embed"

//go:embed index.html
var indexHTML []byte

//go:embed all:portal
var portalFS embed.FS
