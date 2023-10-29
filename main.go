package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aqua/openweather-prometheus-exporter/collector"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	listen       = flag.String("listen", ":9654", "(Host and) port to listen on for Prometheus export")
	dailyOWLimit = flag.Int("daily-openweather-call-limit", 1000,
		"Make no more than this many calls/day to OpenWeather"+
			" (will return stale data when sampled too quickly)")
	openweatherAPIKey = flag.String("openweather-api-key", "",
		"API key for Openweather")
	openweatherAPIKeyFile = flag.String("openweather-api-key-file", "",
		"File containing API key for Openweather")
)

type GeoPoint struct {
	lat, lng float64
}

func (p *GeoPoint) String() string {
	return fmt.Sprintf("%f,%f", p.lat, p.lng)
}

type locationList []GeoPoint

var locations locationList

func (ll *locationList) String() string {
	s := make([]string, len(*ll))
	for i, l := range *ll {
		s[i] = l.String()
	}
	return strings.Join(s, " ")
}

func (ll *locationList) Set(value string) error {
	tok := strings.Split(value, ",")
	if len(tok) != 2 {
		return fmt.Errorf("Unparseable location %q", value)
	}
	lat, laterr := strconv.ParseFloat(strings.TrimSpace(tok[0]), 64)
	lng, lngerr := strconv.ParseFloat(strings.TrimSpace(tok[1]), 64)
	if laterr != nil || lngerr != nil {
		return fmt.Errorf("Unparseable location %q", value)
	}
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return fmt.Errorf("Out of range latitude or longitude %f,%f", lat, lng)
	}
	*ll = append(*ll, GeoPoint{lat, lng})
	return nil
}

func readKeyOrFile(key, keyFile string) (string, error) {
	if key != "" {
		return key, nil
	}
	if keyFile != "" {
		if b, err := ioutil.ReadFile(keyFile); err != nil {
			return "", err
		} else {
			return string(bytes.TrimSpace(b)), nil
		}
	}
	return "", nil
}

func init() {
	flag.Var(&locations, "location", "lat,lng to collect")
}

var conditionMutex sync.Mutex

type ttlCollector struct {
	collector  *collector.Collector
	conditions *collector.Conditions
	timestamp  time.Time
}

var collectors = map[string]*ttlCollector{}

const collectionTTL = 10 * time.Second

func (tc *ttlCollector) reCollect() (*collector.Conditions, error) {
	now := time.Now()
	if !tc.timestamp.IsZero() && now.Sub(tc.timestamp) < collectionTTL {
		return tc.conditions, nil
	} else {
		cond, err := tc.collector.Collect()
		if err == nil {
			tc.conditions = cond
			tc.timestamp = now
		}
		return cond, err
	}
}

func export(tc *ttlCollector, location string) error {
	prometheus.MustRegister(
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace:   "weather",
				Name:        "temperature_celsius",
				Help:        "Current local temperature, in °C",
				ConstLabels: prometheus.Labels{"location": location},
			}, func() float64 {
				conditionMutex.Lock()
				defer conditionMutex.Unlock()
				if cond, err := tc.reCollect(); err != nil {
					return math.NaN()
				} else {
					return cond.Temperature
				}
			}),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace:   "weather",
				Name:        "pressure_hpa",
				Help:        "Current local atmospheric pressure (hectopascals)",
				ConstLabels: prometheus.Labels{"location": location},
			}, func() float64 {
				conditionMutex.Lock()
				defer conditionMutex.Unlock()
				if cond, err := tc.reCollect(); err != nil {
					return math.NaN()
				} else {
					return cond.Pressure
				}
			}),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace:   "weather",
				Name:        "humidity",
				Help:        "Current local humidity",
				ConstLabels: prometheus.Labels{"location": location},
			}, func() float64 {
				conditionMutex.Lock()
				defer conditionMutex.Unlock()
				if cond, err := tc.reCollect(); err != nil {
					return math.NaN()
				} else {
					return cond.Humidity
				}
			}),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace:   "weather",
				Name:        "wind_speed_meters_per_sec",
				Help:        "Current local wind speed, in meters/sec",
				ConstLabels: prometheus.Labels{"location": location},
			}, func() float64 {
				conditionMutex.Lock()
				defer conditionMutex.Unlock()
				if cond, err := tc.reCollect(); err != nil {
					return math.NaN()
				} else {
					return cond.WindSpeed
				}
			}),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace:   "weather",
				Name:        "wind_direction_degrees",
				Help:        "Current local wind direction, in degrees from 0° (North)",
				ConstLabels: prometheus.Labels{"location": location},
			}, func() float64 {
				conditionMutex.Lock()
				defer conditionMutex.Unlock()
				if cond, err := tc.reCollect(); err != nil {
					return math.NaN()
				} else {
					return cond.WindDirection
				}
			}),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace:   "weather",
				Name:        "cloud_cover_percent",
				Help:        "Current local cloud cover, in percent",
				ConstLabels: prometheus.Labels{"location": location},
			}, func() float64 {
				conditionMutex.Lock()
				defer conditionMutex.Unlock()
				if cond, err := tc.reCollect(); err != nil {
					return math.NaN()
				} else {
					return cond.CloudCoverPercent
				}
			}),
	)
	return nil
}

func main() {
	flag.Parse()
	if len(locations) == 0 {
		log.Printf("At least one --location is required")
		os.Exit(2)
	}
	if *openweatherAPIKey == "" && *openweatherAPIKeyFile == "" {
		log.Printf("One of --openweather-api-key or --openweather-api-key-file is required")
		os.Exit(2)
	}
	k, err := readKeyOrFile(*openweatherAPIKey, *openweatherAPIKeyFile)
	if err != nil {
		log.Fatalf("Error reading openweather key: %v", err)
	}

	for _, l := range locations {
		ls := l.String()
		collectors[ls] = &ttlCollector{
			collector: collector.NewCollector(k, *dailyOWLimit, l.lat, l.lng),
		}
		export(collectors[ls], ls)
	}
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(*listen, nil))
}
