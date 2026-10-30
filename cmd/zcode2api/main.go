package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"zcode2api/internal/config"
	"zcode2api/internal/gateway"
)

// printBanner mirrors the GLM-ZAI-2API startup banner.
func printBanner() {
	cyan, yellow, magenta, green, reset := "\033[36m", "\033[33m", "\033[35m", "\033[32m", "\033[0m"
	fmt.Println()
	fmt.Println(cyan + "  ███████╗ ██████╗ ██████╗ ██████╗ ███████╗██████╗  █████╗ ██████╗ ██╗" + reset)
	fmt.Println(cyan + "  ╚══███╔╝██╔════╝██╔═══██╗██╔══██╗██╔════╝╚════██║██╔══██╗██╔══██╗██║" + reset)
	fmt.Println(cyan + "    ███╔╝ ██║     ██║   ██║██║  ██║█████╗   █████╔╝███████║██████╔╝██║" + reset)
	fmt.Println(cyan + "   ███╔╝  ██║     ██║   ██║██║  ██║██╔══╝  ██╔═══╝ ██╔══██║██╔═══╝ ██║" + reset)
	fmt.Println(cyan + "  ███████╗╚██████╗╚██████╔╝██████╔╝███████╗███████╗██║  ██║██║     ██║" + reset)
	fmt.Println(cyan + "  ╚═════╝  ╚═════╝ ╚═════╝ ╚═════╝ ╚═════╝ ╚═════╝ ╚═╝  ╚═╝╚═╝     ╚═╝" + reset)
	fmt.Println()
	fmt.Println(yellow + "  📧 Telegram:" + reset + " https://t.me/D3_vin")
	fmt.Println(magenta + "  👤 Author:" + reset + " @D3vin_dev")
	fmt.Println(green + "  🔗 GitHub:" + reset + " https://github.com/D3-vin/Zcode2Api")
	fmt.Println(cyan + "  📦 Version:" + reset + " " + config.AppVersion)
	fmt.Println()
}

func main() {
	printBanner()
	config.LoadDotEnv()
	// LOG_FILE: empty (default) = console only; a path = logs ALSO go to
	// that file (truncated each start) — the console keeps printing in parallel
	if lf := strings.TrimSpace(os.Getenv("LOG_FILE")); lf != "" {
		if f, err := os.OpenFile(lf, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
			log.SetOutput(io.MultiWriter(f, os.Stderr))
			fmt.Println("  logging to " + lf)
		} else {
			fmt.Println("  LOG_FILE unavailable, console logging: " + err.Error())
		}
	}
	// This machine routes egress through a local mitmproxy via env vars
	// (http_proxy=127.0.0.1:18787, SSL_CERT_FILE=mitm CA). zcode.z.ai direct
	// egress serves the real Sectigo chain, so scrub the mitm env: the server
	// must go direct with system roots like in production.
	config.ScrubMitmEnv()
	cfg := config.LoadConfig()
	s, err := gateway.NewServer(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	log.Fatal(s.ListenAndServe())
}
