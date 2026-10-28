package modules

import (
	"fmt"
	"os"
	"testing"
)

// TestMain replaces HOME with a temporary directory: manager commands read
// ~/.config/futi/config.yaml, and without the replacement tests would pick up
// the sources of the user's real config.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "futi-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
