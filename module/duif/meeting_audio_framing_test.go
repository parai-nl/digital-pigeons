package duif

import (
	"bytes"
	"io"
	"testing"
)

// sizedReader levert een body in vooraf bepaalde stukken: precies de
// willekeurige knips die de HTTP-lezer live maakte (4096, 4095 en 1 byte per
// browserblok van 8192).
type sizedReader struct {
	data  []byte
	sizes []int
}

func (r *sizedReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := len(r.data)
	if len(r.sizes) > 0 {
		n, r.sizes = r.sizes[0], r.sizes[1:]
	}
	n = min(n, len(r.data), len(p))
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}

func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func collect(body io.Reader) [][]byte {
	var frames [][]byte
	splitAudioFrames(body, func(f []byte) { frames = append(frames, f) })
	return frames
}

// Een browserblok van 4096 monsters dat de lezer in 4096+4095+1 bytes knipt
// hoort als twee volle frames van 4096 bytes de brug in te gaan: geen oneven
// frame, geen verschoven monster, geen verloren byte.
func TestSplitAudioFrames_OddReadsStillEndOnSampleBoundary(t *testing.T) {
	body := pattern(2 * audioChunkBytes)
	frames := collect(&sizedReader{data: body, sizes: []int{4096, 4095, 1}})
	if len(frames) != 2 {
		t.Fatalf("%d frames, verwacht 2", len(frames))
	}
	var got []byte
	for i, f := range frames {
		if len(f) != audioChunkBytes {
			t.Errorf("frame %d heeft %d bytes, verwacht %d", i, len(f), audioChunkBytes)
		}
		got = append(got, f...)
	}
	if !bytes.Equal(got, body) {
		t.Error("de doorgegeven audio wijkt af van de body")
	}
}

// Een byte-voor-byte gelezen body geeft hetzelfde resultaat: de framegrens
// hangt niet van de leesgrootte af.
func TestSplitAudioFrames_OneByteReads(t *testing.T) {
	body := pattern(audioChunkBytes + 6)
	frames := collect(iotestOneByte{r: bytes.NewReader(body)})
	if len(frames) != 2 || len(frames[0]) != audioChunkBytes || len(frames[1]) != 6 {
		t.Fatalf("frames %v, verwacht [%d 6]", lens(frames), audioChunkBytes)
	}
}

// Het restant gaat met een even lengte mee; een halve monsterbyte vervalt.
func TestSplitAudioFrames_TrailingHalfSampleIsDropped(t *testing.T) {
	frames := collect(bytes.NewReader(pattern(7)))
	if len(frames) != 1 || len(frames[0]) != 6 {
		t.Fatalf("frames %v, verwacht [6]", lens(frames))
	}
	if frames := collect(bytes.NewReader(pattern(1))); len(frames) != 0 {
		t.Fatalf("één losse byte hoort geen frame te geven, kreeg %v", lens(frames))
	}
}

type iotestOneByte struct{ r io.Reader }

func (o iotestOneByte) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return o.r.Read(p[:1])
}

func lens(frames [][]byte) []int {
	out := make([]int, len(frames))
	for i, f := range frames {
		out[i] = len(f)
	}
	return out
}
