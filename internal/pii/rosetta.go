package pii

import (
	"github.com/istoo/owl/internal/rosetta"
	"github.com/istoo/owl/internal/winarm"
)

// translated says owl runs under Rosetta: an Intel build on Apple
// silicon. Tests replace it.
var translated = rosetta.Translated

// emulated says owl runs under Windows on Arm's x64 emulation: an x64
// build on an Arm64 computer. Tests replace it.
var emulated = winarm.Emulated
