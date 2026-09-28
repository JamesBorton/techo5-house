//go:build !dot && !spot

package camera

// Luma is the frame as a coarse grid of brightness: cols x rows zone means of the raw green samples on
// the 10-bit scale, row by row. It reads the sensor's packed frame directly, the way the exposure
// meter does, so it costs a few thousand byte reads and no development at all. It is for comparing
// frames with each other (did anything move?), not for looking at.
func (f *Frame) Luma(cols, rows int) []float64 {
	f.mu.Lock()
	raw := f.raw
	f.mu.Unlock()
	if raw == nil || cols <= 0 || rows <= 0 {
		return nil
	}
	return lumaGrid(raw, cols, rows)
}

// lumaGrid samples every meterRowStep-th sensor row and every meterGroupStep-th five-byte group, the
// same green pixel the meter reads, into cols x rows zones.
func lumaGrid(bayer []byte, cols, rows int) []float64 {
	sum := make([]float64, cols*rows)
	count := make([]float64, cols*rows)
	groups := sensorW / 4
	for y := 0; y+1 < sensorH; y += meterRowStep {
		off := y * bytesPerLine
		if off >= len(bayer) {
			break
		}
		line := bayer[off:]
		zy := y * rows / sensorH
		for g := 0; g < groups; g += meterGroupStep {
			i := g * 5
			if i+2 >= len(line) {
				break
			}
			v := float64(uint16(line[i+1])>>2 | (uint16(line[i+2])&0xF)<<6)
			zx := g * cols / groups
			sum[zy*cols+zx] += v
			count[zy*cols+zx]++
		}
	}
	for i := range sum {
		if count[i] > 0 {
			sum[i] /= count[i]
		}
	}
	return sum
}
