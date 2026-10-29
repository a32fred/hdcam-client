package audio

import (
	"encoding/binary"
	"fmt"
)

var adpcmSteps = [...]int32{
	7, 8, 9, 10, 11, 12, 13, 14, 16, 17, 19, 21, 23, 25, 28, 31,
	34, 37, 41, 45, 50, 55, 60, 66, 73, 80, 88, 97, 107, 118, 130, 143,
	157, 173, 190, 209, 230, 253, 279, 307, 337, 371, 408, 449, 494, 544,
	598, 658, 724, 796, 876, 963, 1060, 1166, 1282, 1411, 1552, 1707,
	1878, 2066, 2272, 2499, 2749, 3024, 3327, 3660, 4026, 4428, 4871,
	5358, 5894, 6484, 7132, 7845, 8630, 9493, 10442, 11487, 12635,
	13899, 15289, 16818, 18500, 20350, 22385, 24623, 27086, 29794, 32767,
}

var adpcmIndexAdjust = [...]int{-1, -1, -1, -1, 2, 4, 6, 8}

// DecodeADPCM decodes one independently reset camera audio payload to PCM16LE.
func DecodeADPCM(payload []byte) ([]byte, error) {
	if len(payload) == 0 || len(payload) > 2048 {
		return nil, fmt.Errorf("ADPCM payload length must be 1..2048 bytes: %d", len(payload))
	}
	pcm := make([]byte, len(payload)*4)
	var predictor int32
	index := 0
	sample := 0
	for _, packed := range payload {
		for _, nibble := range [...]byte{packed >> 4, packed & 15} {
			step := adpcmSteps[index]
			magnitude := nibble & 7
			// Separate integer divisions preserve this camera's rounding.
			delta := step*int32(magnitude)/4 + step/8
			if nibble&8 != 0 {
				delta = -delta
			}
			predictor += delta
			if predictor > 32767 {
				predictor = 32767
			}
			if predictor < -32768 {
				predictor = -32768
			}
			binary.LittleEndian.PutUint16(pcm[sample*2:], uint16(int16(predictor)))
			sample++
			index += adpcmIndexAdjust[magnitude]
			if index < 0 {
				index = 0
			}
			if index > 88 {
				index = 88
			}
		}
	}
	return pcm, nil
}
