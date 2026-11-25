//go:build !windows

package icons

import "fmt"

func extractExecutableIconPNG(_ string) ([]byte, error) {
	return nil, fmt.Errorf("executable icon extraction requires Windows")
}
