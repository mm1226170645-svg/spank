//go:build !cgo

package main

import (
	"context"
	"fmt"
)

type micConfig struct {
	threshold   float64
	multiplier  float64
	highpassHz  float64
	noiseCancel bool
}

func listenForMicSlaps(ctx context.Context, pack *soundPack, tuning runtimeTuning, cfg micConfig) error {
	_ = ctx
	_ = pack
	_ = tuning
	_ = cfg
	return fmt.Errorf("mic mode requires CGO; rebuild with CGO_ENABLED=1")
}
