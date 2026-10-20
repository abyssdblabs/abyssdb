// Package nulltime is AbyssDB's model of time and space.
//
// It provides interplanetary regions with real(ish) orbital mechanics, light
// travel times for NullTime commit-wait, and a warp factor, because demos are
// not allowed to take 44 minutes even when physics insists.
package nulltime

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	speedOfLight = 299_792.458     // km/s
	au           = 149_597_870.700 // km
)

var j2000 = time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC)

// Warp compresses every interplanetary wait by this factor. Production
// deployments must run at Warp 1. We cannot enforce this, but we are asking.
var Warp = 1.0

// Sleep sleeps for d, compressed by Warp. It returns the duration actually slept.
func Sleep(d time.Duration) time.Duration {
	actual := Warped(d)
	time.Sleep(actual)
	return actual
}

// Warped returns d compressed by Warp.
func Warped(d time.Duration) time.Duration {
	if Warp <= 1 {
		return d
	}
	return time.Duration(float64(d) / Warp)
}

// orbit is a circular, coplanar heliocentric orbit. Kepler would be
// disappointed. Kepler is not our target audience.
type orbit struct {
	a      float64 // semi-major axis, AU
	period float64 // days
	l0     float64 // mean longitude at J2000, degrees
}

func (o orbit) position(t time.Time) (x, y float64) {
	days := t.Sub(j2000).Hours() / 24
	l := (o.l0 + 360*days/o.period) * math.Pi / 180
	return o.a * math.Cos(l), o.a * math.Sin(l)
}

var (
	earth   = orbit{1.00000, 365.256, 100.464}
	mars    = orbit{1.52368, 686.980, 355.453}
	jupiter = orbit{5.20260, 4332.59, 34.396}
	saturn  = orbit{9.55491, 10759.22, 49.954}
)

type Region struct {
	Name        string
	Body        string
	Description string
	distance    func(t time.Time) float64 // km from Earth
}

// Terrestrial reports whether the region is on Earth, where light is fast
// enough for us to blame the network instead.
func (r Region) Terrestrial() bool { return r.Body == "Earth" }

// Distance returns the distance from Earth in km at time t.
func (r Region) Distance(t time.Time) float64 { return r.distance(t) }

// OneWayLightTime is the minimum possible replication latency to the region.
func (r Region) OneWayLightTime(t time.Time) time.Duration {
	return time.Duration(r.distance(t) / speedOfLight * float64(time.Second))
}

func planet(o orbit) func(time.Time) float64 {
	return func(t time.Time) float64 {
		ex, ey := earth.position(t)
		px, py := o.position(t)
		return math.Hypot(px-ex, py-ey) * au
	}
}

func fixed(km float64) func(time.Time) float64 {
	return func(time.Time) float64 { return km }
}

// Voyager 1 crosses one light-day from Earth around November 2026, moving
// outward at ~17 km/s. We ignore Earth's orbit; Voyager has bigger problems.
var voyagerOneLightDay = time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)

func voyager(t time.Time) float64 {
	return speedOfLight*86400 + 17.0*t.Sub(voyagerOneLightDay).Seconds()
}

var regions = map[string]Region{}

func register(r Region) { regions[r.Name] = r }

func init() {
	for _, name := range []string{"us-east-1", "us-west-2", "eu-central-1", "eu-west-1", "ap-southeast-1"} {
		register(Region{name, "Earth", "a terrestrial region (light is basically instant here)", fixed(0)})
	}
	register(Region{"luna-shackleton-1", "Moon", "lunar south pole; permanently shadowed, naturally cold storage", fixed(384_400)})
	register(Region{"mars-olympus-1", "Mars", "Olympus Mons; highest availability zone in the solar system", planet(mars)})
	register(Region{"mars-jezero-1", "Mars", "Jezero Crater; co-located with Perseverance (no data sharing agreement)", planet(mars)})
	register(Region{"jupiter-europa-1", "Europa", "subsurface ocean; liquid-cooled", planet(jupiter)})
	register(Region{"saturn-titan-1", "Titan", "methane lakes; the only region with weather worse than us-east-1", planet(saturn)})
	register(Region{"voyager-1", "Voyager 1", "interstellar edge location", voyager})
}

// Lookup finds a region by name.
func Lookup(name string) (Region, error) {
	r, ok := regions[name]
	if !ok {
		return Region{}, fmt.Errorf("unknown region %q (known: %s)", name, strings.Join(Names(), ", "))
	}
	return r, nil
}

// Names lists all regions, nearest first.
func Names() []string {
	now := time.Now()
	out := make([]string, 0, len(regions))
	for n := range regions {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		di, dj := regions[out[i]].Distance(now), regions[out[j]].Distance(now)
		if di != dj {
			return di < dj
		}
		return out[i] < out[j]
	})
	return out
}

// FormatDistance renders km in the most impressive applicable unit.
func FormatDistance(km float64) string {
	switch {
	case km == 0:
		return "0 km"
	case km < 1e6:
		return fmt.Sprintf("%.0f km", km)
	case km < 1e11:
		return fmt.Sprintf("%.2f AU", km/au)
	default:
		return fmt.Sprintf("%.3f light-days", km/speedOfLight/86400)
	}
}

// FormatDuration renders long durations as h/m/s rather than Go's default.
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Microsecond).String()
	}
	return d.Round(time.Second).String()
}
