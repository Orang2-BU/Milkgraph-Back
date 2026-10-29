package trace

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Vector records source volumes in litres, not percentages.
type Vector map[string]float64

type Transfer struct {
	At       time.Time
	From, To string
	Volume   float64
}

// Propagate applies perfect-mixture transfers chronologically; equal timestamps
// use input order. All containers (including output lots) must be declared in
// initial. Inputs are copied. Lot proportions follow its source volumes at fill.
// Historical fixture percentages are calibrated temporal contributions, not
// derived by dividing total RMT tank volumes; this engine is for new data.
func Propagate(sources []string, initial map[string]Vector, transfers []Transfer) (map[string]Vector, error) {
	allowedSources := make(map[string]struct{}, len(sources))
	for _, s := range sources {
		allowedSources[s] = struct{}{}
	}
	state := make(map[string]Vector, len(initial))
	for id, vector := range initial {
		state[id] = make(Vector, len(vector))
		for source, volume := range vector {
			if _, ok := allowedSources[source]; !ok {
				return nil, fmt.Errorf("unknown source: %s", source)
			}
			if math.IsNaN(volume) || math.IsInf(volume, 0) || volume < 0 {
				return nil, fmt.Errorf("invalid volume for %s: %v", source, volume)
			}
			state[id][source] = volume
		}
	}
	ordered := append([]Transfer(nil), transfers...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].At.Before(ordered[j].At) })
	for _, transfer := range ordered {
		if transfer.At.IsZero() {
			return nil, fmt.Errorf("zero transfer time")
		}
		if math.IsNaN(transfer.Volume) || math.IsInf(transfer.Volume, 0) || transfer.Volume < 0 {
			return nil, fmt.Errorf("invalid transfer volume: %v", transfer.Volume)
		}
		from, okFrom := state[transfer.From]
		to, okTo := state[transfer.To]
		if !okFrom || !okTo || transfer.From == transfer.To {
			return nil, fmt.Errorf("invalid endpoints: %s -> %s", transfer.From, transfer.To)
		}
		if transfer.Volume == 0 {
			continue
		}
		total := 0.0
		for _, volume := range from {
			total += volume
		}
		if math.IsInf(total, 0) {
			return nil, fmt.Errorf("tank volume overflow")
		}
		if transfer.Volume > total {
			return nil, fmt.Errorf("outgoing volume %v exceeds tank volume %v", transfer.Volume, total)
		}
		destinationTotal := 0.0
		for _, volume := range to {
			destinationTotal += volume
		}
		if math.IsInf(destinationTotal+transfer.Volume, 0) {
			return nil, fmt.Errorf("destination volume overflow")
		}
		ratio := transfer.Volume / total
		for source, volume := range from {
			moved := volume * ratio
			from[source] -= moved
			to[source] += moved
		}
	}
	return state, nil
}
