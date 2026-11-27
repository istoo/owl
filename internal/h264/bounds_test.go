package h264

import (
	"image"
	"math"
	"testing"
)

// A picture near the limit of an int is refused before its size is
// rounded and multiplied (#L11).
func TestNewEncoderRefusesAnOversizedPicture(t *testing.T) {
	if _, err := NewEncoder(math.MaxInt, 2, 26); err == nil {
		t.Fatal("a picture wider than an int sees was taken")
	}
	if _, err := NewEncoder(2, math.MaxInt, 26); err == nil {
		t.Fatal("a picture taller than an int sees was taken")
	}
	if _, err := NewEncoder(20000, 2, 26); err == nil {
		t.Fatal("a picture over the encoder's bound was taken")
	}
	if _, err := NewEncoder(16, 16, 26); err != nil {
		t.Fatalf("a small picture was refused: %v", err)
	}
}

// A picture whose bounds run backwards has nothing to copy: convert pads
// the frame instead of reading before the image (#L11).
func TestConvertWithABackwardsBounds(t *testing.T) {
	e, err := NewEncoder(16, 16, 26)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(10, 10, 4, 4)) // Dx, Dy negative
	p := e.convert(img)
	if len(p.y) != 16*16 {
		t.Fatalf("plane is %d bytes", len(p.y))
	}
	for i, v := range p.y {
		if v != 16 {
			t.Fatalf("padding byte %d = %d, want 16", i, v)
		}
	}
	// an empty picture takes the same path
	if p := e.convert(image.NewRGBA(image.Rect(0, 0, 0, 0))); len(p.y) != 16*16 {
		t.Fatal("an empty picture made no plane")
	}
}
