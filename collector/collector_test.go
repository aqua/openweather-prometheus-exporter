package collector

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
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

var handleWeather25 = func(calls *atomic.Uint32) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		f, err := os.Open("testdata/weather-2.5.json")
		if err != nil {
			http.Error(w, fmt.Sprintf("error opening testdata/weather-2.5.json: %v", err), http.StatusInternalServerError)
			return
		}
		if _, err = io.Copy(w, f); err != nil {
			http.Error(w, fmt.Sprintf("error reading testdata/weather-2.5.json: %v", err), http.StatusInternalServerError)
			return
		}
	}
}

func setupServer(calls *atomic.Uint32) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/data/2.5/weather", handleWeather25(calls))
	srv := httptest.NewServer(mux)
	os.Setenv("OPEN_WEATHER_ENDPOINT", srv.URL)
	return srv
}

func TestCollect25Once(t *testing.T) {
	calls := &atomic.Uint32{}
	srv := setupServer(calls)
	defer srv.Close()
	c := NewCollector("key here", 1000, 123, 45)
	cond, err := c.Collect()
	if err != nil {
		t.Errorf("Error collecting 2.5 weather: %v", err)
		return
	}
	if cond.Temperature != 20.24 {
		t.Errorf("got temperature %f, want %f", cond.Temperature, 20.24)
	}
	if cond.Humidity != 59 {
		t.Errorf("got humidity %f, want %f", cond.Humidity, 59.)
	}
	if cond.WindSpeed != 4.63 {
		t.Errorf("got wind speed %f, want %f", cond.WindSpeed, 4.63)
	}
	if cond.WindDirection != 320 {
		t.Errorf("got wind direction %f, want %f", cond.WindDirection, 320.)
	}
	if cond.CloudCoverPercent != 75 {
		t.Errorf("got wind direction %f, want %f", cond.CloudCoverPercent, 75.)
	}
}

func TestCollect25Ratelimited(t *testing.T) {
	calls := &atomic.Uint32{}
	srv := setupServer(calls)
	defer srv.Close()
	c := NewCollector("key here", 10, 123, 45)
	for i := 0; i < 10; i++ {
		_, err := c.Collect()
		if err != nil {
			t.Errorf("Error collecting 2.5 weather: %v", err)
			return
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("handler was called %d times, want 1", got)
	}
}
