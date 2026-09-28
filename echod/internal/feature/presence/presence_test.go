//go:build !dot && !spot

package presence

import "testing"

func grid(v float64) []float64 {
	g := make([]float64, gridCols*gridRows)
	for i := range g {
		g[i] = v
	}
	return g
}

// The exposure loop brightening the whole picture is not somebody moving.
func TestAWholePictureGettingBrighterIsNotMovement(t *testing.T) {
	a, b := grid(200), grid(320)
	if got := changed(a, b); got != 0 {
		t.Fatalf("uniform brightening scored %.1f%%, want 0", got)
	}
}

// Somebody walking across a few zones is.
func TestAFewZonesChangingIsMovement(t *testing.T) {
	a, b := grid(200), grid(200)
	for i := 0; i < 10; i++ {
		b[40+i] = 520
	}
	got := changed(a, b)
	if got < float64(10*100)/float64(len(a)) {
		t.Fatalf("ten changed zones scored %.1f%%", got)
	}
}

// A dark room is sensor noise, not a picture; it says nothing either way.
func TestADarkRoomScoresNothing(t *testing.T) {
	a, b := grid(5), grid(5)
	b[0] = 40
	if got := changed(a, b); got != 0 {
		t.Fatalf("dark frames scored %.1f%%, want 0", got)
	}
}

func TestMismatchedGridsScoreNothing(t *testing.T) {
	if got := changed(grid(100), nil); got != 0 {
		t.Fatalf("got %.1f", got)
	}
}
