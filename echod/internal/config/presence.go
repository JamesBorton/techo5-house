package config

// Presence is whether the device tells Home Assistant somebody is in the room, and from what.
type Presence struct {
	// Enabled turns the whole thing on. Off by default: it keeps the camera busy for a moment every
	// little while, which nobody should get without asking for it.
	Enabled bool `json:"enabled"`

	// Camera lets it look for movement as well as listen and feel for touches.
	Camera bool `json:"camera"`

	// Hold is how long, in seconds, the room counts as occupied after the last sign of anybody.
	Hold int `json:"hold_s"`

	// Motion is how much of the picture has to change, in percent of its zones, to count as movement.
	Motion int `json:"motion"`

	// Sound is how loud the room has to be, as a share of "someone talking close by", to count. Zero
	// leaves the microphones out of it.
	Sound int `json:"sound"`
}

const DefaultPresenceHold = 300

const DefaultPresenceMotion = 3

const DefaultPresenceSound = 30

func defaultPresence() Presence {
	return Presence{Camera: true, Hold: DefaultPresenceHold, Motion: DefaultPresenceMotion, Sound: DefaultPresenceSound}
}

type PresenceWriter struct{ st *Store }

func (w PresenceWriter) Enabled(v bool) error {
	return w.st.Update(func(c *Config) { c.Presence.Enabled = v })
}

func (w PresenceWriter) Camera(v bool) error {
	return w.st.Update(func(c *Config) { c.Presence.Camera = v })
}

func (w PresenceWriter) Hold(v int) error {
	return w.st.Update(func(c *Config) { c.Presence.Hold = v })
}

func (w PresenceWriter) Motion(v int) error {
	return w.st.Update(func(c *Config) { c.Presence.Motion = v })
}

func (w PresenceWriter) Sound(v int) error {
	return w.st.Update(func(c *Config) { c.Presence.Sound = v })
}
