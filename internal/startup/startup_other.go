//go:build !windows

package startup

import "errors"

var errWindowsOnly = errors.New("Windows Startup shortcuts are only supported on Windows")

type osBackend struct{}

func (osBackend) Supported() bool { return false }

func (osBackend) ListShortcuts(string) ([]ShortcutMeta, error) { return nil, nil }

func (osBackend) WriteShortcut(ShortcutSpec) error { return errWindowsOnly }

func (osBackend) RemoveShortcut(string) error { return errWindowsOnly }

func (osBackend) IsStartupApproved(string) (bool, error) { return true, nil }

func (osBackend) SetStartupApproved(string, bool) error { return nil }

func (osBackend) OpenFolder(string) error { return errWindowsOnly }
