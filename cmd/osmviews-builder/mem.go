// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"runtime"
)

// memStats returns a compact snapshot of the process's Go memory use, for
// logging. HeapAlloc is the live heap; Sys is the total obtained from the
// operating system, which is the cheapest proxy for what a cgroup memory
// limit accounts for. ReadMemStats briefly stops the world, so call it at
// human timescales (once every 30 s, at stage boundaries), not in hot loops.
func memStats() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return fmt.Sprintf("mem: heap %d MiB, sys %d MiB", m.HeapAlloc>>20, m.Sys>>20)
}
