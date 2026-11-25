//go:build !windows

package icons

func findWindowsShortcutIcon(_ []string) string {
	return ""
}
