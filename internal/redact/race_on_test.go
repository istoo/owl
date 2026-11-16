//go:build race

package redact

// raceOn says the tests run under -race, where sync.Pool lets go of a
// quarter of what it holds: a test that bounds allocations gives it room.
const raceOn = true
