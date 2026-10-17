//go:build !linux

package kernel

import (
	"errors"
	"os"
)

func dropCache(*os.File) {}

func syncAll() {}

// BootDevice exists only on the device.
func BootDevice() (string, error) { return "", errors.New("no boot partition here") }
