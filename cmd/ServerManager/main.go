package main

import (
	"fmt"
	"os"

	"github.com/Mostakim2005/server-manager/internal/cli"
	"github.com/Mostakim2005/server-manager/internal/config"
	"github.com/Mostakim2005/server-manager/internal/server"
)

// ServerManager.exe provides the windowless GUI tray & app launcher:
// - No args: starts daemon + System Tray and opens http://127.0.0.1:8790 (or focuses existing instance).
// - --tray / --background: starts silently in System Tray without opening a browser window.
// - Other CLI verbs: delegates cleanly to headless CLI.
func main() {
	openUI := true
	if len(os.Args) > 1 {
		arg := os.Args[1]
		switch arg {
		case "--tray", "--background":
			openUI = false
		case "--daemon":
		default:
			os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
		}
	}
	addr := fmt.Sprintf("%s:%d", config.DefaultBindHost, config.DefaultPort)
	os.Exit(server.RunForegroundDaemon(addr, openUI, true))
}
