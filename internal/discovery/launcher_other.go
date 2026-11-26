//go:build !windows

package discovery

func findInstalledLauncher(_, _ string) (string, string, string) {
	return "", "", ""
}
