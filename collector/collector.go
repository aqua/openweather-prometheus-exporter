package collector

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"golang.org/x/time/rate"
)

type openweathermapTemperature struct {
	Temperature         float64 `json:"temp"`
	Pressure            float64 `json:"pressure"`
	SeaLevelPressure    float64 `json:"sea_max"`
	GroundLevelPressure float64 `json:"grnt_max"`
	HumidityPercent     int     `json:"humidity"`
	TemperatureMin      float64 `json:"temp_min"`
	TemperatureMax      float64 `json:"temp_max"`
}

type openweathermapWind struct {
	Speed     float64 `json:"speed"`
	Direction float64 `json:"deg"`
}

type miConverterFunc func(float64) float64

func (w openweathermapWind) Format(tc miConverterFunc, units string) string {
	return fmt.Sprintf("%s at %.1f%s", windDirection(w.Direction), tc(w.Speed), units)
}

type openweathermapWeather struct {
	ID          int    `json:"id"`
	Main        string `json:"main"`
	Description string `json:"description"`
}

type openweathermapClouds struct {
	CoverPercent float64 `json:"all"`
}

type openweathermapPrecipitation struct {
	OneHour   float64 `json:"1h"`
	ThreeHour float64 `json:"3h"`
}

type openweathermapLocation struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lon"`
}

type Openweathermap25ConditionsResponse struct {
	Location  openweathermapLocation      `json:"coord"`
	Weather   []openweathermapWeather     `json:"weather"`
	Main      openweathermapTemperature   `json:"main"`
	Wind      openweathermapWind          `json:"wind"`
	Clouds    openweathermapClouds        `json:"clouds"`
	Rain      openweathermapPrecipitation `json:"rain"`
	Snow      openweathermapPrecipitation `json:"snow"`
	Timestamp int64                       `json:"dt"`
	CityName  string                      `json:"name"`
}

func (c *Openweathermap25ConditionsResponse) toConditions() *Conditions {
	return &Conditions{
		Temperature:       c.Main.Temperature,
		Humidity:          float64(c.Main.HumidityPercent),
		WindSpeed:         c.Wind.Speed,
		WindDirection:     c.Wind.Direction,
		CloudCoverPercent: c.Clouds.CoverPercent,
	}
}

type Openweathermap30Conditions struct {
	Sunrise       int64   `json:"sunrise"`
	Sunset        int64   `json:"sunset"`
	Moonrise      int64   `json:"moonrise"`
	Moonset       int64   `json:"moonset"`
	MoonPhase     float64 `json:"moon_phase"`
	Temperature   float64 `json:"temp"`
	FeelsLike     float64 `json:"feels_like"`
	Pressure      int     `json:"pressure"`
	Humidity      float64 `json:"humidity"`
	DewPoint      float64 `json:"dew_point"`
	UVI           float64 `json:"uvi"`
	CloudCover    float64 `json:"clouds"`
	Visibility    int64   `json:"visibility"`
	WindSpeed     float64 `json:"wind_speed"`
	WindDirection float64 `json:"wind_deg"`
	WindGusts     float64 `json:"wind_gust"`
}

func (c *Openweathermap30Conditions) toConditions() *Conditions {
	return &Conditions{
		Temperature:       c.Temperature,
		Humidity:          c.Humidity,
		WindSpeed:         c.WindSpeed,
		WindDirection:     c.WindDirection,
		CloudCoverPercent: c.CloudCover,
	}
}

func (c Openweathermap30Conditions) SunriseTime() time.Time  { return time.Unix(c.Sunrise, 0) }
func (c Openweathermap30Conditions) SunsetTime() time.Time   { return time.Unix(c.Sunset, 0) }
func (c Openweathermap30Conditions) MoonriseTime() time.Time { return time.Unix(c.Moonrise, 0) }
func (c Openweathermap30Conditions) MoonsetTime() time.Time  { return time.Unix(c.Moonset, 0) }

type Openweathermap30ConditionsResponse struct {
	Timezone       string `json:"timezone"`
	TimezoneOffset int    `json:"timezone_offset"`

	Lat     float64                     `json:"lat"`
	Lon     float64                     `json:"lon"`
	Current *Openweathermap30Conditions `json:"current"`
}

func (r *Openweathermap30ConditionsResponse) LocationString() string {
	ll := fmt.Sprintf("%f,%f", r.Lat, r.Lon)
	return ll
}

func windDirection(deg float64) string {
	seq := []string{
		"N", "NNE", "NE", "ENE",
		"E", "ESE", "SE", "SSE",
		"S", "SSW", "SW", "WSW",
		"W", "WNW", "NW", "NNW",
	}
	for i, dir := range seq {
		p := 22.5 * float64(i)
		if deg >= p && deg < p+22.5 {
			return dir
		}
	}
	return "unknown"
}

var globalLimiter *rate.Limiter

const (
	OPENWEATHER_API_2_5 = iota
	OPENWEATHER_API_3_0 = iota
)

type Collector struct {
	apiVersion     int
	lat, lng       float64
	apiKey         string
	limiter        *rate.Limiter
	lastConditions *Conditions
}

func (c Collector) String() string {
	return fmt.Sprintf("%f,%f", c.lat, c.lng)
}

func NewCollector(key string, dailyLimit int, lat, lng float64) *Collector {
	interval := time.Second * time.Duration(86400/dailyLimit)
	log.Printf("allowing 1 call per %s", interval)
	return &Collector{
		apiVersion: OPENWEATHER_API_2_5,
		apiKey:     key,
		limiter:    rate.NewLimiter(rate.Every(interval), 1),
		lat:        lat,
		lng:        lng,
	}
}

func openWeatherEndpoint() string {
	if e := os.Getenv("OPEN_WEATHER_ENDPOINT"); e != "" {
		return e
	}
	return "https://api.openweathermap.org"
}

func (c *Collector) get25Conditions() (*Conditions, error) {
	u := fmt.Sprintf(
		"%s/data/2.5/weather?lat=%f&lon=%f&APPID=%s&units=metric",
		openWeatherEndpoint(), c.lat, c.lng, url.QueryEscape(c.apiKey))
	log.Printf("calling 2.5 API at %s", u)
	resp, err := http.Get(u)
	if err != nil {
		log.Printf("Error calling openweather: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	wr := &Openweathermap25ConditionsResponse{}
	if err = dec.Decode(wr); err != nil {
		log.Printf("Error decoding openweather 2.5 response: %v", err)
		return nil, err
	}
	return wr.toConditions(), nil
}

func (c *Collector) get30Conditions() (*Conditions, error) {
	u := fmt.Sprintf(
		"%s/data/3.0/onecall?lat=%f&lon=%f&appid=%s&exclude=minutely,daily,hourly,alerts&units=metric",
		openWeatherEndpoint(), c.lat, c.lng, url.QueryEscape(c.apiKey))
	log.Printf("Calling 3.0 API at %s", u)
	resp, err := http.Get(u)
	if err != nil {
		log.Printf("Error calling openweather: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	wr := &Openweathermap30ConditionsResponse{}
	if err = dec.Decode(wr); err != nil {
		log.Printf("Error decoding openweather response: %v", err)
		return nil, err
	}
	return wr.Current.toConditions(), nil
}

func (c *Collector) Conditions() (*Conditions, error) {
	switch c.apiVersion {
	case OPENWEATHER_API_2_5:
		return c.get25Conditions()
	case OPENWEATHER_API_3_0:
		return c.get30Conditions()
	default:
		return nil, fmt.Errorf("Unsupported openweather API version %d", c.apiVersion)
	}
}

type Conditions struct {
	Temperature       float64
	Humidity          float64
	WindSpeed         float64
	WindDirection     float64
	CloudCoverPercent float64
}

func (c *Collector) Collect() (*Conditions, error) {
	if c.limiter.Allow() {
		log.Printf("under rate limit, allowing API call")
		current, err := c.Conditions()
		if err == nil {
			c.lastConditions = current
		}
		return current, err
	} else if c.lastConditions == nil {
		return nil, fmt.Errorf("Rate limited, but no previous conditions to reuse")
	}
	log.Printf("ratelimited, reusing last result")
	return c.lastConditions, nil
}
