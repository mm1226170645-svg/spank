//go:build darwin && cgo

package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"time"
	"unsafe"

	"github.com/gen2brain/malgo"
)

type micConfig struct {
	threshold  float64
	multiplier float64
}

type micEvent struct {
	rms float64
	at  time.Time
}

func listenForMicSlaps(ctx context.Context, pack *soundPack, tuning runtimeTuning, cfg micConfig) error {
	if cfg.threshold <= 0 {
		cfg.threshold = defaultMicThreshold
	}
	if cfg.multiplier <= 0 {
		cfg.multiplier = defaultMicMultiplier
	}

	malgoCtx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return fmt.Errorf("mic init: %w", err)
	}
	defer func() {
		_ = malgoCtx.Uninit()
		malgoCtx.Free()
	}()

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = 1
	deviceConfig.SampleRate = 44100

	sizeInBytes := uint32(malgo.SampleSizeInBytes(deviceConfig.Capture.Format))
	events := make(chan micEvent, 16)

	var lastTrigger time.Time
	noiseEMA := cfg.threshold
	const noiseAlpha = 0.01

	onRecvFrames := func(_ []byte, input []byte, framecount uint32) {
		if len(input) == 0 {
			return
		}
		sampleCount := framecount * deviceConfig.Capture.Channels
		if sampleCount == 0 {
			return
		}

		var sum float64
		if len(input) >= int(sampleCount*sizeInBytes) {
			samples := unsafe.Slice((*int16)(unsafe.Pointer(&input[0])), sampleCount)
			for _, v := range samples {
				fv := float64(v) / 32768.0
				sum += fv * fv
			}
		} else {
			for i := 0; i+1 < len(input); i += 2 {
				v := int16(binary.LittleEndian.Uint16(input[i : i+2]))
				fv := float64(v) / 32768.0
				sum += fv * fv
			}
			sampleCount = uint32(len(input) / 2)
			if sampleCount == 0 {
				return
			}
		}

		rms := math.Sqrt(sum / float64(sampleCount))
		threshold := math.Max(cfg.threshold, noiseEMA*cfg.multiplier)

		now := time.Now()
		if rms > threshold && now.Sub(lastTrigger) > tuning.cooldown {
			lastTrigger = now
			select {
			case events <- micEvent{rms: rms, at: now}:
			default:
			}
			return
		}

		if rms < threshold {
			noiseEMA = (1-noiseAlpha)*noiseEMA + noiseAlpha*rms
		}
	}

	device, err := malgo.InitDevice(malgoCtx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: onRecvFrames,
	})
	if err != nil {
		return fmt.Errorf("mic device init: %w", err)
	}
	defer device.Uninit()

	if err := device.Start(); err != nil {
		return fmt.Errorf("mic device start: %w", err)
	}

	tracker := newSlapTracker(pack, tuning.cooldown)
	speakerInit := false

	fmt.Printf("spank: mic mode enabled (make a sharp tap near the mic, ctrl+c to quit)\n")
	if stdioMode {
		fmt.Println(`{"status":"ready","mode":"mic"}`)
	}

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nbye!")
			return nil
		case ev := <-events:
			num, score := tracker.record(ev.at)
			file := tracker.getFile(score)
			amp := micRMSAmplitude(ev.rms)
			if stdioMode {
				event := map[string]interface{}{
					"timestamp":  ev.at.Format(time.RFC3339Nano),
					"slapNumber": num,
					"amplitude":  amp,
					"severity":   "mic",
					"file":       file,
					"rms":        ev.rms,
				}
				if data, err := json.Marshal(event); err == nil {
					fmt.Println(string(data))
				}
			} else {
				fmt.Printf("thak #%d [rms=%.4f] -> %s\n", num, ev.rms, file)
			}
			go playAudio(pack, file, amp, &speakerInit)
		}
	}
}

func micRMSAmplitude(rms float64) float64 {
	amp := rms * 8.0
	if amp < 0.05 {
		amp = 0.05
	}
	if amp > 1.0 {
		amp = 1.0
	}
	return amp
}
