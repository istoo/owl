//go:build !race

package redact

// raceOn says the tests run under -race, where sync.Pool lets go of what
// it holds more often: a test that bounds allocations gives it room.
const raceOn = false
