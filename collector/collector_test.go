package collector

import (
	"testing"
)

func TestWindDirection(t *testing.T) {
	cases := []struct {
		deg  float64
		want string
	}{
		{0, "N"},
		{22.6, "NNE"},
		{45.1, "NE"},
	}
	for _, c := range cases {
		if got := windDirection(c.deg); got != c.want {
			t.Errorf("for wind direction %f, got %s, want %s", c.deg, got, c.want)
		}
	}
}
