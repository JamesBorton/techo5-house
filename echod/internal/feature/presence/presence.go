//go:build !dot && !spot

// Package presence tells Home Assistant whether somebody is in the room.
//
// It adds no sensing of its own. It listens to what the device already notices: a touch on the screen,
// a conversation, the room being louder than its own floor, and, when allowed, the camera seeing
// something move. The camera is the expensive one, so it is looked through for a moment every little
// while rather than watched: a short burst, two frames compared, and the sensor let go.
//
// Occupancy is held for a while after the last sign of anybody, since people sit still and rooms go
// quiet with people in them. Off until switched on.
package presence

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/camera"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/lenscover"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/mic"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
	"github.com/HuskerMinion/techo5/echod/internal/service"
)

func init() {
	component.Register(component.Device, Get(), component.Order(70),
		component.Supervise(service.Restart(time.Second, 30*time.Second)))
}

const (
	tick = 250 * time.Millisecond

	// How often the camera is looked through: often while the room is empty, so somebody arriving
	// is noticed, and less often while it is occupied, since then it only has to keep the hold alive.
	lookEmpty    = 20 * time.Second
	lookOccupied = 45 * time.Second

	// settle is how many frames a cold sensor is given for its exposure to find the room, and gap how
	// many frames apart the two compared pictures are.
	settle = 6
	gap    = 15

	// burstWait is the most a look may take before it is abandoned.
	burstWait = 5 * time.Second

	gridCols, gridRows = 16, 12

	// zoneChange is how much one zone's share of the picture's brightness has to move to count as
	// changed, and tooDark the mean below which the picture is noise rather than a room.
	zoneChange = 0.25
	tooDark    = 12

	// soundRun is how many ticks in a row the room has to be over the sound level to count, and
	// speakerQuiet how long after the device's own playback the microphones are believed again.
	soundRun     = 3
	speakerQuiet = 2 * time.Second
)

type Presence struct {
	occupied *esphome.BinarySensor
	enable   *esphome.Switch
	useCam   *esphome.Switch
	hold     *esphome.Number
	motion   *esphome.Number
	sound    *esphome.Number
	score    *esphome.Sensor
	cue      *esphome.TextSensor

	mu       sync.Mutex
	lastSeen time.Time
	present  bool

	// looking is set while a camera burst is out, so the next is not started on top of it. camDead is
	// set once the sensor has gone for the rest of the boot, and nothing tries it again.
	looking atomic.Bool
	camDead atomic.Bool

	// ref is the last look's second frame, to compare the next look's first against.
	ref []float64
}

var (
	once   sync.Once
	shared *Presence
)

func Get() *Presence {
	once.Do(func() { shared = build() })
	return shared
}

func build() *Presence {
	p := &Presence{
		occupied: &esphome.BinarySensor{
			Base:        esphome.Base{ObjectID: "presence", Name: "Presence", Icon: "mdi:account-eye"},
			DeviceClass: "occupancy",
		},
		enable: &esphome.Switch{Base: esphome.Base{
			ObjectID: "presence_detection", Name: "Presence detection", Icon: "mdi:account-search",
			Category: esphome.CategoryConfig,
		}},
		useCam: &esphome.Switch{Base: esphome.Base{
			ObjectID: "presence_camera", Name: "Presence uses the camera", Icon: "mdi:camera-iris",
			Category: esphome.CategoryConfig,
		}},
		hold: &esphome.Number{
			Base: esphome.Base{
				ObjectID: "presence_hold", Name: "Presence hold", Icon: "mdi:timer-sand",
				Category: esphome.CategoryConfig,
			},
			Min: 1, Max: 60, Step: 1, Unit: "min", Mode: esphome.NumberBox,
		},
		motion: &esphome.Number{
			Base: esphome.Base{
				ObjectID: "presence_motion", Name: "Presence motion threshold", Icon: "mdi:motion-sensor",
				Category: esphome.CategoryConfig,
			},
			Min: 1, Max: 50, Step: 1, Unit: "%", Mode: esphome.NumberBox,
		},
		sound: &esphome.Number{
			Base: esphome.Base{
				ObjectID: "presence_sound", Name: "Presence sound threshold", Icon: "mdi:ear-hearing",
				Category: esphome.CategoryConfig,
			},
			Min: 0, Max: 100, Step: 5, Unit: "%", Mode: esphome.NumberBox,
		},
		score: &esphome.Sensor{
			Base: esphome.Base{
				ObjectID: "presence_motion_score", Name: "Presence motion score", Icon: "mdi:chart-bell-curve",
				Category: esphome.CategoryDiagnostic,
			},
			Unit: "%", StateClass: esphome.StateClassMeasurement,
		},
		cue: &esphome.TextSensor{Base: esphome.Base{
			ObjectID: "presence_cue", Name: "Presence last cue", Icon: "mdi:account-question",
			Category: esphome.CategoryDiagnostic,
		}},
	}

	p.enable.OnCommand = func(on bool) {
		p.enable.Set(on)
		save("enabled", config.Set().Presence().Enabled(on))
		if !on {
			p.clear()
		}
	}
	p.useCam.OnCommand = func(on bool) {
		p.useCam.Set(on)
		save("camera", config.Set().Presence().Camera(on))
	}
	p.hold.OnCommand = func(v float32) {
		p.hold.Set(v)
		save("hold", config.Set().Presence().Hold(int(v)))
	}
	p.motion.OnCommand = func(v float32) {
		p.motion.Set(v)
		save("motion", config.Set().Presence().Motion(int(v)))
	}
	p.sound.OnCommand = func(v float32) {
		p.sound.Set(v)
		save("sound", config.Set().Presence().Sound(int(v)))
	}

	// The cheap signs arrive on their own; the camera and the room's level are looked for in Run.
	touch.Get().Gestures.Listen(func(touch.Gesture) { p.seen("touch") })
	voice.Changed.Listen(func(s voice.State) {
		if s.Phase != "idle" {
			p.seen("voice")
		}
	})
	return p
}

func save(what string, err error) {
	if err != nil {
		slog.Error("presence: save setting", "what", what, "err", err)
	}
}

func (p *Presence) Name() string { return "presence" }

func (p *Presence) Entities() []esphome.Entity {
	return []esphome.Entity{p.occupied, p.enable, p.useCam, p.hold, p.motion, p.sound, p.score, p.cue}
}

func (p *Presence) Restore(c config.Config) {
	pc := c.Presence
	p.enable.Set(pc.Enabled)
	p.useCam.Set(pc.Camera)
	p.hold.Set(float32(pc.Hold))
	p.motion.Set(float32(pc.Motion))
	p.sound.Set(float32(pc.Sound))
	p.occupied.Set(false)
	slog.Info("restored", "what", "presence", "enabled", pc.Enabled, "camera", pc.Camera,
		"hold_min", pc.Hold, "motion_pct", pc.Motion, "sound_pct", pc.Sound)
}

// seen is a sign of somebody. It is called from other components' goroutines, so it only records.
func (p *Presence) seen(why string) {
	if !config.Get().Presence.Enabled {
		return
	}
	p.mu.Lock()
	p.lastSeen = time.Now()
	was := p.present
	p.present = true
	p.mu.Unlock()
	p.cue.Set(why)
	if !was {
		p.occupied.Set(true)
		slog.Info("presence: occupied", "cue", why)
	}
}

func (p *Presence) clear() {
	p.mu.Lock()
	was := p.present
	p.present = false
	p.mu.Unlock()
	if was {
		p.occupied.Set(false)
		slog.Info("presence: empty")
	}
}

func (p *Presence) Run(ctx context.Context) error {
	t := time.NewTicker(tick)
	defer t.Stop()

	var (
		nextLook  time.Time
		loud      int
		written   = speaker.Get().Written()
		lastSound time.Time
	)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		pc := config.Get().Presence
		if !pc.Enabled {
			continue
		}
		now := time.Now()

		// The room's level, unless the device itself has just been making the noise.
		if w := speaker.Get().Written(); w != written {
			written, lastSound = w, now
		}
		if pc.Sound > 0 && now.Sub(lastSound) > speakerQuiet &&
			mic.Get().Level() >= float64(pc.Sound)/100 {
			if loud++; loud >= soundRun {
				p.seen("sound")
				loud = 0
			}
		} else {
			loud = 0
		}

		// The hold.
		p.mu.Lock()
		present, last := p.present, p.lastSeen
		p.mu.Unlock()
		if present && now.Sub(last) > time.Duration(pc.Hold)*time.Minute {
			p.clear()
			present = false
		}

		// The camera, now and then.
		if pc.Camera && !p.camDead.Load() && now.After(nextLook) && !p.looking.Load() {
			if present {
				nextLook = now.Add(lookOccupied)
			} else {
				nextLook = now.Add(lookEmpty)
			}
			p.looking.Store(true)
			go func(threshold int) {
				defer p.looking.Store(false)
				p.look(ctx, threshold)
			}(pc.Motion)
		}
	}
}

// look takes two frames a moment apart and says whether anything moved between them, or since the
// last look.
func (p *Presence) look(ctx context.Context, threshold int) {
	cam := camera.Get()
	if err := cam.Wedged(); err != nil {
		p.camDead.Store(true)
		slog.Warn("presence: camera gone for this boot; carrying on without it", "err", err)
		return
	}
	if lc := lenscover.Get(); lc.Present() && lc.Covered() {
		return
	}
	release, err := cam.Acquire()
	if err != nil {
		// Muted, shutter closed, or on its way down: all of them pass on their own.
		return
	}
	defer release()

	frames := make(chan *camera.Frame, 4)
	cancel := cam.Frames.Listen(func(f *camera.Frame) {
		select {
		case frames <- f:
		default:
		}
	})
	defer cancel()

	ctx, stop := context.WithTimeout(ctx, burstWait)
	defer stop()
	var first, second []float64
	n := 0
	for second == nil {
		select {
		case <-ctx.Done():
			return
		case f := <-frames:
			n++
			switch {
			case n == settle:
				first = f.Luma(gridCols, gridRows)
			case n == settle+gap:
				second = f.Luma(gridCols, gridRows)
			}
		}
	}

	now := changed(first, second)
	since := 0.0
	if p.ref != nil {
		since = changed(p.ref, first) / 2 // the room between looks: light drifts, so it counts for half
	}
	p.ref = second
	score := math.Max(now, since)
	p.score.Set(float32(score))
	if score >= float64(threshold) {
		p.seen("camera")
	}
}

// changed is the percentage of zones whose share of the picture's brightness moved between a and b.
// Each grid is divided by its own mean first, so the exposure loop brightening or darkening the whole
// picture does not count as movement.
func changed(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	ma, mb := mean(a), mean(b)
	if ma < tooDark || mb < tooDark {
		return 0
	}
	count := 0
	for i := range a {
		na, nb := a[i]/ma, b[i]/mb
		base := math.Max((na+nb)/2, 0.1)
		if math.Abs(na-nb)/base > zoneChange {
			count++
		}
	}
	return float64(count) * 100 / float64(len(a))
}

func mean(v []float64) float64 {
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}
