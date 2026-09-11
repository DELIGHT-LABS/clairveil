// Package secretmem provides best-effort clearing of owned secret scratch.
// It cannot erase compiler, register, stack-growth or runtime copies.
package secretmem

import "runtime"

// Clear overwrites the pointed-to value. Keeping the non-inlined call and
// pointer alive prevents the caller's dead-store elimination from deleting
// the owned-memory overwrite; it does not promise complete memory erasure.
//
//go:noinline
func Clear[T any](value *T) {
	var zero T
	*value = zero
	runtime.KeepAlive(value)
}
