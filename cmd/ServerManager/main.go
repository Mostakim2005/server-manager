package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Mostakim2005/server-manager/internal/cli"
	"github.com/Mostakim2005/server-manager/internal/config"
	"github.com/Mostakim2005/server-manager/internal/server"
)

// ServerManager.exe provides the windowless GUI tray & app launcher:
// - No args: starts daemon + System Tray and opens http://127.0.0.1:8790 (or focuses existing instance).
// - --tray / --background: starts silently in System Tray without opening a browser window.
// - --launch-service <id>: launches a configured service silently (for Windows Startup) and exits immediately.
// - Other CLI verbs: delegates cleanly to headless CLI.
func main() {
	openUI := true
	if len(os.Args) > 1 {
		arg := os.Args[1]
		switch {
		case arg == "--tray" || arg == "--background":
			openUI = false
		case arg == "--daemon":
		case arg == "--launch-service":
			if len(os.Args) < 3 {
				os.Exit(2)
			}
			os.Exit(cli.LaunchService(os.Args[2], os.Stderr))
		case strings.HasPrefix(arg, "--launch-service="):
			os.Exit(cli.LaunchService(strings.TrimPrefix(arg, "--launch-service="), os.Stderr))
		default:
			os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
		}
	}
	addr := fmt.Sprintf("%s:%d", config.DefaultBindHost, config.DefaultPort)
	os.Exit(server.RunForegroundDaemon(addr, openUI, true))
}
