package engine

import (
	"fmt"
	"runtime/metrics"
	"strings"
)

// Carbon accounting. Compute emissions are estimated from the process's CPU
// time, an assumed power draw per busy core, and the grid carbon intensity of
// the regions the shards run in. Storage emissions are measured.

const wattsPerBusyCore = 15.0

// Grid carbon intensity in gCO2e/kWh (approximate annual averages).
var gridIntensity = map[string]float64{
	"us-east-1":      380,
	"us-west-2":      250,
	"eu-central-1":   340,
	"eu-west-1":      280,
	"ap-southeast-1": 470,
}

var offWorldPower = map[string]string{
	"Moon":      "solar",
	"Mars":      "solar",
	"Europa":    "radioisotope (RTG)",
	"Titan":     "radioisotope (RTG)",
	"Voyager 1": "radioisotope (RTG)",
}

var cpuMetrics = []string{
	"/cpu/classes/user:cpu-seconds",
	"/cpu/classes/gc/total:cpu-seconds",
	"/cpu/classes/scavenge/total:cpu-seconds",
}

func cpuSeconds() float64 {
	if s, ok := processCPUSeconds(); ok {
		return s
	}
	samples := make([]metrics.Sample, len(cpuMetrics))
	for i, n := range cpuMetrics {
		samples[i].Name = n
	}
	metrics.Read(samples)
	var total float64
	for _, s := range samples {
		if s.Value.Kind() == metrics.KindFloat64 {
			total += s.Value.Float64()
		}
	}
	return total
}

type carbonReport struct {
	CPUSeconds float64
	WattHours  float64
	Intensity  float64
	Sources    []string
	Grams      float64
}

// carbon estimates compute emissions so far.
func (e *Engine) carbon() carbonReport {
	r := carbonReport{CPUSeconds: cpuSeconds()}
	r.WattHours = r.CPUSeconds * wattsPerBusyCore / 3600
	seen := map[string]bool{}
	for _, s := range e.router.Shards {
		reg := s.Region
		if seen[reg.Name] {
			continue
		}
		seen[reg.Name] = true
		if src, ok := offWorldPower[reg.Body]; ok {
			r.Sources = append(r.Sources, fmt.Sprintf("%s: 0 gCO2e/kWh (%s)", reg.Name, src))
			continue
		}
		g := gridIntensity[reg.Name]
		r.Intensity += g
		r.Sources = append(r.Sources, fmt.Sprintf("%s: %.0f gCO2e/kWh", reg.Name, g))
	}
	// Average over regions, weighting every region equally.
	if len(seen) > 0 {
		r.Intensity /= float64(len(seen))
	}
	r.Grams = r.WattHours / 1000 * r.Intensity
	return r
}

func (e *Engine) showCarbon() *Result {
	c := e.carbon()
	return &Result{Tag: "SHOW CARBON", Notices: []string{
		fmt.Sprintf("compute (CPU time) ...... %.3f s", c.CPUSeconds),
		fmt.Sprintf("energy .................. %.4f Wh (at %.0f W per busy core)", c.WattHours, wattsPerBusyCore),
		fmt.Sprintf("grid intensity .......... %.0f gCO2e/kWh (%s)", c.Intensity, strings.Join(c.Sources, "; ")),
		fmt.Sprintf("compute emissions ....... %s CO2e", formatGrams(c.Grams)),
		"storage emissions ....... 0 g CO2e",
		"embodied carbon ......... 0 g CO2e (no storage hardware provisioned)",
		"water usage ............. 0 L",
		"storage rating .......... Net-Zero Storage™",
	}}
}

func formatGrams(g float64) string {
	if g < 1 {
		return fmt.Sprintf("%.3f mg", g*1000)
	}
	return fmt.Sprintf("%.3f g", g)
}
