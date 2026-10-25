package main

import (
	"bytes"
	"encoding/binary"
	"math"
)

var chime = makeChime()

// makeChime sintetizza un "ding-dong" morbido (WAV in memoria).
func makeChime() []byte {
	const rate = 22050
	notes := []struct {
		f     float64
		start float64
	}{{987.77, 0}, {1318.51, 0.11}}
	total := rate * 3 / 4
	samples := make([]float64, total)
	for _, n := range notes {
		s0 := int(n.start * rate)
		for i := s0; i < total; i++ {
			t := float64(i-s0) / rate
			env := math.Exp(-t*7) * math.Min(1, t*400)
			v := math.Sin(2*math.Pi*n.f*t) + 0.25*math.Sin(2*math.Pi*n.f*2*t) + 0.08*math.Sin(2*math.Pi*n.f*3*t)
			samples[i] += v * env * 0.22
		}
	}
	var pcm bytes.Buffer
	for _, s := range samples {
		binary.Write(&pcm, binary.LittleEndian, int16(math.Max(-1, math.Min(1, s))*32767))
	}
	var w bytes.Buffer
	w.WriteString("RIFF")
	binary.Write(&w, binary.LittleEndian, uint32(36+pcm.Len()))
	w.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(rate), uint32(rate * 2), uint16(2), uint16(16)} {
		binary.Write(&w, binary.LittleEndian, v)
	}
	w.WriteString("data")
	binary.Write(&w, binary.LittleEndian, uint32(pcm.Len()))
	w.Write(pcm.Bytes())
	return w.Bytes()
}
