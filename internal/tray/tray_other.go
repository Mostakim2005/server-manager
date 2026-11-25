//go:build !windows

package tray

import "errors"

// Callbacks wires tray events on Windows.
type Callbacks struct {
	DashboardURL     string
	OnInstallDesktop func()
	IsStartupEnabled func() bool
	OnToggleStartup  func(enable bool)
	OnQuit           func()
}

// Controller is a stub on non-Windows platforms.
type Controller struct{}

// OpenDashboard is a no-op stub on non-Windows test hosts.
func OpenDashboard(url string) error {
	return nil
}

// Start is a no-op stub on non-Windows test hosts.
func Start(cb Callbacks) *Controller {
	return &Controller{}
}

// Stop is a no-op stub on non-Windows test hosts.
func (c *Controller) Stop() {}

// InstallDesktopShortcut is unsupported outside Windows.
func InstallDesktopShortcut(desktopDir, icoPath, targetExe, workDir string) (string, error) {
	return "", errors.New("desktop shortcut installation is Windows-only")
}
