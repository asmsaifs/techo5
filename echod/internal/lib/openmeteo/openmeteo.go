// Package openmeteo is the device's own weather, for a device with no Home Assistant to ask: Open-Meteo's
// forecast and geocoding APIs, free and without a key (open-meteo.com; data CC BY 4.0).
package openmeteo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	forecastURL = "https://api.open-meteo.com/v1/forecast"
	geocodeURL  = "https://geocoding-api.open-meteo.com/v1/search"
	client      = &http.Client{Timeout: 15 * time.Second}
)

// Place is a match for a search.
type Place struct {
	Name     string // "Anchorage, Alaska"
	Lat, Lon float64
	Country  string // ISO code
	Zone     string // IANA time zone
}

// Now is the weather at this moment.
type Now struct {
	Condition string // Home Assistant's condition names: sunny, clear-night, partlycloudy, ...
	Temp      float64
}

// Day is one day of the forecast.
type Day struct {
	When      time.Time
	Condition string
	High, Low float64
	Rain      int // chance of precipitation, percent; -1 unknown
}

func get(ctx context.Context, base string, q url.Values, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "TECHO5 (github.com/HuskerMinion/techo5)")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("open-meteo: %s: %.200s", resp.Status, strings.TrimSpace(string(b)))
	}
	return json.Unmarshal(b, into)
}

// Find looks a place up by town or postal code. country ("US") narrows it, and is what makes a bare
// ZIP code mean the American one.
func Find(ctx context.Context, name, country string) ([]Place, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("open-meteo: nothing to look for")
	}
	q := url.Values{"name": {name}, "count": {"5"}, "language": {"en"}, "format": {"json"}}
	if country != "" {
		q.Set("countryCode", country)
	}
	var out struct {
		Results []struct {
			Name        string  `json:"name"`
			Admin1      string  `json:"admin1"`
			Country     string  `json:"country"`
			CountryCode string  `json:"country_code"`
			Latitude    float64 `json:"latitude"`
			Longitude   float64 `json:"longitude"`
			Timezone    string  `json:"timezone"`
		} `json:"results"`
	}
	if err := get(ctx, geocodeURL, q, &out); err != nil {
		return nil, err
	}
	var ps []Place
	for _, r := range out.Results {
		n := r.Name
		if r.Admin1 != "" && r.Admin1 != r.Name {
			n += ", " + r.Admin1
		} else if r.Country != "" {
			n += ", " + r.Country
		}
		ps = append(ps, Place{Name: n, Lat: r.Latitude, Lon: r.Longitude, Country: strings.ToUpper(r.CountryCode), Zone: r.Timezone})
	}
	return ps, nil
}

// Forecast is the weather now and the next days (today first) at a place.
func Forecast(ctx context.Context, lat, lon float64, fahrenheit bool, days int) (Now, []Day, error) {
	unit := "celsius"
	if fahrenheit {
		unit = "fahrenheit"
	}
	q := url.Values{
		"latitude":         {strconv.FormatFloat(lat, 'f', 4, 64)},
		"longitude":        {strconv.FormatFloat(lon, 'f', 4, 64)},
		"current":          {"temperature_2m,weather_code,is_day"},
		"daily":            {"weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max"},
		"temperature_unit": {unit},
		"timezone":         {"auto"},
		"forecast_days":    {strconv.Itoa(days)},
	}
	var out struct {
		Current struct {
			Temp  float64 `json:"temperature_2m"`
			Code  int     `json:"weather_code"`
			IsDay int     `json:"is_day"`
		} `json:"current"`
		Daily struct {
			Time []string   `json:"time"`
			Code []int      `json:"weather_code"`
			Max  []float64  `json:"temperature_2m_max"`
			Min  []float64  `json:"temperature_2m_min"`
			Rain []*float64 `json:"precipitation_probability_max"`
		} `json:"daily"`
	}
	if err := get(ctx, forecastURL, q, &out); err != nil {
		return Now{}, nil, err
	}
	now := Now{Condition: Condition(out.Current.Code, out.Current.IsDay == 1), Temp: out.Current.Temp}
	d := out.Daily
	var ds []Day
	for i, t := range d.Time {
		if i >= len(d.Code) || i >= len(d.Max) || i >= len(d.Min) {
			break
		}
		when, err := time.ParseInLocation("2006-01-02", t, time.Local)
		if err != nil {
			continue
		}
		rain := -1
		if i < len(d.Rain) && d.Rain[i] != nil {
			rain = int(*d.Rain[i])
		}
		ds = append(ds, Day{When: when, Condition: Condition(d.Code[i], true), High: d.Max[i], Low: d.Min[i], Rain: rain})
	}
	return now, ds, nil
}

// Condition is a WMO weather code as Home Assistant names the condition, which is what the screen draws.
func Condition(code int, day bool) string {
	switch code {
	case 0:
		if !day {
			return "clear-night"
		}
		return "sunny"
	case 1:
		if !day {
			return "clear-night"
		}
		return "partlycloudy"
	case 2:
		return "partlycloudy"
	case 3:
		return "cloudy"
	case 45, 48:
		return "fog"
	case 51, 53, 55, 61, 63, 80, 81:
		return "rainy"
	case 65, 82:
		return "pouring"
	case 56, 57, 66, 67:
		return "snowy-rainy"
	case 71, 73, 75, 77, 85, 86:
		return "snowy"
	case 95:
		return "lightning-rainy"
	case 96, 99:
		return "hail"
	}
	return "exceptional"
}
