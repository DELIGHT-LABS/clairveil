package auditfield

import (
	"fmt"
	"runtime"

	frct "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/frct"
)

// sponge is the suite-2 fixed-rate SAFE sponge. It is deliberately private:
// callers use the typed audit encrypt/decrypt functions rather than supplying
// arbitrary domains or associated data.
type sponge struct {
	state    [Poseidon2Width]frct.Element
	absorbed int
	squeezed int
	expected []uint32
	calls    []uint32
}

// clear removes owned key-dependent state on every exit. This is best effort:
// Go may retain compiler/runtime copies outside this object's storage.
//
//go:noinline
func (s *sponge) clear() { clear(s.state[:]); clear(s.calls); *s = sponge{}; runtime.KeepAlive(s) }

const (
	ioAbsorb = uint32(0x80000000)
	ioRate   = Poseidon2Rate
)

func ioAbsorbWord(n int) uint32 { return ioAbsorb | uint32(n) }

func newFixedSponge(tag [2]frct.Element, expected []uint32) sponge {
	return sponge{state: [Poseidon2Width]frct.Element{frct.Zero(), tag[0], tag[1]}, expected: append([]uint32(nil), expected...)}
}

func (s *sponge) absorb(values ...frct.Element) {
	for i := range values {
		if s.absorbed == ioRate {
			s.permute()
			s.absorbed = 0
		}
		s.state[s.absorbed].Add(&s.state[s.absorbed], &values[i])
		s.absorbed++
	}
	s.calls = append(s.calls, ioAbsorbWord(len(values)))
	s.squeezed = ioRate
}

func (s *sponge) squeeze() frct.Element {
	if s.squeezed == ioRate {
		s.permute()
		s.squeezed = 0
		s.absorbed = 0
	}
	v := s.state[s.squeezed]
	s.squeezed++
	s.calls = append(s.calls, 1)
	return v
}

func (s *sponge) finish() error {
	if len(s.calls) != len(s.expected) {
		return fmt.Errorf("SAFE IO schedule mismatch")
	}
	for i := range s.calls {
		if s.calls[i] != s.expected[i] {
			return fmt.Errorf("SAFE IO schedule mismatch")
		}
	}
	return nil
}

func (s *sponge) permute() {
	poseidonExternal(&s.state)
	constants := Poseidon2RoundConstants()
	constantIndex := 0
	for round := 0; round < Poseidon2FullRounds+Poseidon2PartialRounds; round++ {
		full := round < Poseidon2FullRounds/2 || round >= Poseidon2FullRounds/2+Poseidon2PartialRounds
		width := 1
		if full {
			width = Poseidon2Width
		}
		for i := 0; i < width; i++ {
			constant, ok := frct.FromCanonicalBE(constants[constantIndex])
			if !ok { // literals are covered by T13 and cannot fail at runtime.
				panic("invalid pinned Poseidon2 constant")
			}
			s.state[i].Add(&s.state[i], &constant)
			constantIndex++
		}
		if full {
			for i := range s.state {
				poseidonSBox(&s.state[i])
			}
			poseidonExternal(&s.state)
		} else {
			poseidonSBox(&s.state[0])
			poseidonInternal(&s.state)
		}
	}
}

func poseidonSBox(value *frct.Element) {
	var square, fourth, fifth frct.Element
	square.Square(value)
	fourth.Square(&square)
	fifth.Mul(&fourth, value)
	value.Set(&fifth)
}

// The external matrix has diagonal 2 and off-diagonal 1.
func poseidonExternal(state *[Poseidon2Width]frct.Element) {
	var sum frct.Element
	sum.Add(&state[0], &state[1])
	sum.Add(&sum, &state[2])
	for i := range state {
		state[i].Add(&state[i], &sum)
	}
}

// The internal matrix is all ones plus diagonal (1, 1, 2).
func poseidonInternal(state *[Poseidon2Width]frct.Element) {
	var sum, twice frct.Element
	sum.Add(&state[0], &state[1])
	sum.Add(&sum, &state[2])
	state[0].Add(&state[0], &sum)
	state[1].Add(&state[1], &sum)
	twice.Add(&state[2], &state[2])
	state[2].Add(&twice, &sum)
}

func fixedTag(index int) ([2]frct.Element, error) {
	if index < 0 || index >= len(StaticTags()) {
		return [2]frct.Element{}, fmt.Errorf("invalid fixed SAFE tag")
	}
	literals := StaticTags()[index]
	var tag [2]frct.Element
	for i := range tag {
		value, ok := frct.FromCanonicalBE(literals[i])
		if !ok {
			return [2]frct.Element{}, fmt.Errorf("invalid pinned SAFE tag")
		}
		tag[i] = value
	}
	return tag, nil
}

func hash2(tagIndex int, values []frct.Element) ([2]frct.Element, error) {
	tag, err := fixedTag(tagIndex)
	if err != nil {
		return [2]frct.Element{}, err
	}
	s := newFixedSponge(tag, []uint32{ioAbsorbWord(len(values)), 1, 1})
	defer s.clear()
	s.absorb(values...)
	result := [2]frct.Element{s.squeeze(), s.squeeze()}
	if err := s.finish(); err != nil {
		return [2]frct.Element{}, err
	}
	return result, nil
}
