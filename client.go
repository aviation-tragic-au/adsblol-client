// Package adsblol is a client for the ADSB.lol API (https://api.adsb.lol),
// a free, community-run ADS-B aggregator that serves readsb's v2 aircraft
// format (the adsbv2 module). It needs no key, but is rate limited per IP
// address; a 429 comes back as an error matching ErrRateLimited.
package adsblol

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aviation-tragic-au/adsbv2"
	"go.uber.org/zap"
)

const (
	// DefaultURL is ADSB.lol's API base, used when Config.URL is empty.
	DefaultURL = "https://api.adsb.lol"

	// DefaultTimeout bounds a request when Config.HTTPClient is unset.
	DefaultTimeout = 10 * time.Second

	// MaxRadiusNM is the largest radius the API accepts.
	MaxRadiusNM = 250
)

var (
	// ErrNotFound is returned by GetClosestAircraft when no aircraft is
	// within the radius.
	ErrNotFound = errors.New("adsblol: no aircraft found")

	// ErrRateLimited matches, under errors.Is, a 429. The error is an
	// *adsbv2.RateLimitError carrying the Retry-After delay.
	ErrRateLimited = adsbv2.ErrRateLimited

	squawkPattern = regexp.MustCompile(`^[0-7]{4}$`)
)

// Aircraft is an aircraft in readsb's v2 format.
type Aircraft = adsbv2.Aircraft

// Config configures a Client. Everything is optional. The mapstructure
// tags let a host decode it straight from viper or similar.
type Config struct {
	// URL is the API base. Empty means DefaultURL.
	URL string `json:"url" mapstructure:"url"`

	// UserAgent identifies the program to ADSB.lol, which asks API users
	// to send one. Empty means adsbv2.DefaultUserAgent.
	UserAgent string `json:"userAgent" mapstructure:"userAgent"`

	// HTTPClient sends the requests. Nil means a client with
	// DefaultTimeout.
	HTTPClient *http.Client `json:"-" mapstructure:"-"`
}

// Client is a client for the ADSB.lol API. It is safe for concurrent use.
type Client struct {
	logger     *zap.Logger
	httpClient *http.Client
	url        string
	userAgent  string
}

// NewClient creates an ADSB.lol client. A nil logger logs nothing.
func NewClient(logger *zap.Logger, cfg Config) (*Client, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	base := strings.TrimRight(cfg.URL, "/")
	if base == "" {
		base = DefaultURL
	}
	if _, err := url.Parse(base); err != nil {
		return nil, fmt.Errorf("adsblol: invalid URL: %w", err)
	}

	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = adsbv2.DefaultUserAgent
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}

	return &Client{
		logger:     logger.Named("adsblol"),
		httpClient: httpClient,
		url:        base,
		userAgent:  userAgent,
	}, nil
}

// GetAircraftWithinRange returns every aircraft within radiusNM nautical
// miles (1 to MaxRadiusNM) of lat/lon.
func (c *Client) GetAircraftWithinRange(ctx context.Context, lat, lon float64, radiusNM int) ([]*Aircraft, error) {
	path, err := pointPath("point", lat, lon, radiusNM)
	if err != nil {
		return nil, err
	}
	return c.list(ctx, path)
}

// GetClosestAircraft returns the aircraft closest to lat/lon within
// radiusNM nautical miles, or ErrNotFound.
func (c *Client) GetClosestAircraft(ctx context.Context, lat, lon float64, radiusNM int) (*Aircraft, error) {
	path, err := pointPath("closest", lat, lon, radiusNM)
	if err != nil {
		return nil, err
	}

	aircraft, err := c.list(ctx, path)
	if err != nil {
		return nil, err
	}
	if len(aircraft) == 0 || aircraft[0] == nil {
		return nil, ErrNotFound
	}
	return aircraft[0], nil
}

// GetAircraftByHex returns the aircraft with a 24-bit ICAO address in hex
// ("7c6b2d"). The list is empty when none is being tracked.
func (c *Client) GetAircraftByHex(ctx context.Context, hex string) ([]*Aircraft, error) {
	return c.lookup(ctx, "hex", strings.ToLower(strings.TrimSpace(hex)))
}

// GetAircraftByCallsign returns the aircraft using a callsign ("QFA615").
func (c *Client) GetAircraftByCallsign(ctx context.Context, callsign string) ([]*Aircraft, error) {
	return c.lookup(ctx, "callsign", strings.ToUpper(strings.TrimSpace(callsign)))
}

// GetAircraftByRegistration returns the aircraft with a registration
// ("VH-VZS").
func (c *Client) GetAircraftByRegistration(ctx context.Context, registration string) ([]*Aircraft, error) {
	return c.lookup(ctx, "registration", strings.ToUpper(strings.TrimSpace(registration)))
}

// GetAircraftByType returns the aircraft of an ICAO type designator
// ("B738", "A320").
func (c *Client) GetAircraftByType(ctx context.Context, aircraftType string) ([]*Aircraft, error) {
	return c.lookup(ctx, "type", strings.ToUpper(strings.TrimSpace(aircraftType)))
}

// GetAircraftBySquawk returns the aircraft squawking a code: four octal
// digits, as a string so a leading zero ("0123") survives.
func (c *Client) GetAircraftBySquawk(ctx context.Context, squawk string) ([]*Aircraft, error) {
	if !squawkPattern.MatchString(squawk) {
		return nil, fmt.Errorf("adsblol: invalid squawk %q", squawk)
	}
	return c.list(ctx, "/v2/sqk/"+squawk)
}

// GetMilitaryAircraft returns the aircraft the database marks as military.
func (c *Client) GetMilitaryAircraft(ctx context.Context) ([]*Aircraft, error) {
	return c.list(ctx, "/v2/mil")
}

// GetLADDAircraft returns the aircraft on the FAA's LADD (Limiting Aircraft
// Data Displayed) list.
func (c *Client) GetLADDAircraft(ctx context.Context) ([]*Aircraft, error) {
	return c.list(ctx, "/v2/ladd")
}

// GetPIAAircraft returns the aircraft using a privacy ICAO address.
func (c *Client) GetPIAAircraft(ctx context.Context) ([]*Aircraft, error) {
	return c.list(ctx, "/v2/pia")
}

// lookup fetches /v2/<kind>/<code>.
func (c *Client) lookup(ctx context.Context, kind, code string) ([]*Aircraft, error) {
	if code == "" {
		return nil, fmt.Errorf("adsblol: a %s is required", kind)
	}
	return c.list(ctx, "/v2/"+kind+"/"+url.PathEscape(code))
}

// list sends a GET for path and returns the v2 envelope's aircraft.
func (c *Client) list(ctx context.Context, path string) ([]*Aircraft, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("adsblol: unable to build request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("adsblol: request failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	out, err := adsbv2.Decode(res)
	if err != nil {
		c.logger.Debug("request failed", zap.String("path", path), zap.Error(err))
		return nil, fmt.Errorf("adsblol: %w", err)
	}

	return out.Aircraft, nil
}

// pointPath builds /v2/<kind>/<lat>/<lon>/<radius>, validating each.
func pointPath(kind string, lat, lon float64, radiusNM int) (string, error) {
	if !valid(lat, 90) || !valid(lon, 180) {
		return "", fmt.Errorf("adsblol: invalid coordinates %v,%v", lat, lon)
	}
	if radiusNM <= 0 || radiusNM > MaxRadiusNM {
		return "", fmt.Errorf("adsblol: radius must be 1-%d nm, got %d", MaxRadiusNM, radiusNM)
	}
	return fmt.Sprintf("/v2/%s/%.5f/%.5f/%d", kind, lat, lon, radiusNM), nil
}

// valid reports whether v is a finite number within ±limit.
func valid(v, limit float64) bool {
	return !math.IsNaN(v) && v >= -limit && v <= limit
}
