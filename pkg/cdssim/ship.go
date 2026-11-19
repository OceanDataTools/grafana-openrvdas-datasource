package cdssim

import (
	"math"
	"time"
)

// wave is a sine with the given amplitude and period in seconds, offset by
// phase radians.
func wave(t, amp, period, phase float64) float64 {
	return amp * math.Sin(2*math.Pi*t/period+phase)
}

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

// ShipFields is a small ship's worth of fields named after the OpenRVDAS
// NBP sample data: 10 Hz attitude from the S330 GNSS/INS, 1 Hz navigation and
// met, and a 10 s average. Values are smooth, deterministic functions of time.
// The provisioned dashboard and e2e tests depend on these names and units.
func ShipFields() []Field {
	const fast, slow = 100 * time.Millisecond, time.Second
	heading := func(t float64) float64 {
		return math.Mod(90+wave(t, 8, 300, 0)+wave(t, 1.5, 9, 1)+360, 360)
	}
	airTemp := func(t float64) float64 { return 2.5 + wave(t, 1.2, 900, 0) + wave(t, 0.1, 37, 2) }
	return []Field{
		{Name: "S330HeadingTrue", Units: "degrees", Description: "True heading", Period: fast,
			Value: func(t float64) any { return round(heading(t), 2) }},
		{Name: "S330Pitch", Units: "degrees", Description: "Pitch, bow up positive", Period: fast,
			Value: func(t float64) any { return round(wave(t, 2, 7.3, 0)+wave(t, 0.5, 2.1, 1), 3) }},
		{Name: "S330Roll", Units: "degrees", Description: "Roll, starboard down positive", Period: fast,
			Value: func(t float64) any { return round(wave(t, 6, 10.7, 0)+wave(t, 1, 3.3, 2), 3) }},
		{Name: "S330Heave", Units: "m", Description: "Heave, up positive", Period: fast,
			Value: func(t float64) any { return round(wave(t, 1.2, 8, 0)+wave(t, 0.3, 3.1, 1), 3) }},
		{Name: "S330CourseTrue", Units: "degrees", Description: "Course over ground", Period: slow,
			Value: func(t float64) any { return round(math.Mod(heading(t)+wave(t, 2, 60, 0)+360, 360), 1) }},
		{Name: "S330SpeedKt", Units: "knots", Description: "Speed over ground", Period: slow,
			Value: func(t float64) any { return round(10+wave(t, 0.4, 120, 0), 2) }},
		{Name: "MwxAirTemp", Units: "C", Description: "Air temperature", Period: slow,
			Value: func(t float64) any { return round(airTemp(t), 2) }},
		{Name: "MwxBaroPress", Units: "mbar", Description: "Barometric pressure", Period: slow,
			Value: func(t float64) any { return round(1004+wave(t, 3, 3600, 0), 1) }},
		{Name: "MwxRelWindDir", Units: "degrees", Description: "Relative wind direction", Period: slow,
			Value: func(t float64) any { return round(math.Mod(30+wave(t, 15, 90, 0)+360, 360), 1) }},
		{Name: "MwxRelWindSpeed", Units: "knots", Description: "Relative wind speed", Period: slow,
			Value: func(t float64) any { return round(18+wave(t, 4, 45, 0)+wave(t, 1, 7, 1), 1) }},
		{Name: "AvgMwxAirTemp", Units: "C", Description: "Air temperature, 10 s average", Period: 10 * time.Second,
			Value: func(t float64) any { return round(airTemp(t), 2) }},
	}
}
