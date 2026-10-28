package main

import (
	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/executor"
	"github.com/tivjnb/futi/internal/modules"
)

// Manager performs module management actions over a Runtime. I/O goes through
// Streams only.
type Manager struct {
	rt *core.Runtime
	s  modules.Streams
}

// New returns a Manager over rt writing to the given streams.
func New(rt *core.Runtime, s modules.Streams) *Manager {
	return &Manager{rt: rt, s: s}
}

// moduleRoots returns the roots scanned by list, update and uninstall.
func (m *Manager) moduleRoots() ([]moduleRoot, error) {
	return moduleRootsFrom(m.rt.Config(), m.rt.ModulesDir(), m.rt.LocalDir()), nil
}

// commandConfig returns the base executor config for child processes started
// by the manager (git, curl), following the runtime exec policy.
func (m *Manager) commandConfig() executor.Config {
	return executor.Config{
		Stdin:          m.s.Stdin,
		Stdout:         m.s.Out,
		Stderr:         m.s.Err,
		ForwardSignals: m.rt.Exec().ForwardSignals,
		Group:          m.rt.Exec().CommandGroup,
	}
}
