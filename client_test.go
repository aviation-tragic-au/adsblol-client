package adsblol

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aviation-tragic-au/adsbv2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// server starts a test server that checks the path and User-Agent, then
// answers with status and body. It returns a client pointed at it.
func server(t *testing.T, path string, status int, body string) *Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, path, r.URL.EscapedPath())
		assert.Equal(t, "test-agent/1.0", r.Header.Get("User-Agent"))
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(nil, Config{URL: srv.URL, UserAgent: "test-agent/1.0"})
	require.NoError(t, err)
	return c
}

func TestNewClient(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		c, err := NewClient(nil, Config{})
		require.NoError(t, err)
		assert.Equal(t, DefaultURL, c.url)
		assert.Equal(t, adsbv2.DefaultUserAgent, c.userAgent)
		assert.Equal(t, DefaultTimeout, c.httpClient.Timeout)
	})

	t.Run("invalid URL", func(t *testing.T) {
		_, err := NewClient(nil, Config{URL: "http://bad\x7f"})
		require.Error(t, err)
	})

	t.Run("own HTTP client", func(t *testing.T) {
		hc := &http.Client{}
		c, err := NewClient(nil, Config{HTTPClient: hc})
		require.NoError(t, err)
		assert.Same(t, hc, c.httpClient)
	})
}

func TestGetAircraftWithinRange(t *testing.T) {
	c := server(t, "/v2/point/-27.38420/153.11750/50", 200,
		`{"ac":[{"hex":"7c7a3b","flight":"VOZ1385 ","alt_baro":3500,"gs":180.5,"lat":-27.1,"lon":153.0},
		        {"hex":"7c1089","alt_baro":"ground"}],"msg":"No error"}`)

	ac, err := c.GetAircraftWithinRange(context.Background(), -27.3842, 153.1175, 50)
	require.NoError(t, err)
	require.Len(t, ac, 2)
	assert.Equal(t, "VOZ1385", ac[0].Callsign())
	assert.InDelta(t, 180.5, *ac[0].GS, 1e-9)
	_, onGround := ac[1].Altitude()
	assert.True(t, onGround)
}

func TestPointValidation(t *testing.T) {
	c, err := NewClient(nil, Config{URL: "http://unused.test"})
	require.NoError(t, err)

	tests := []struct {
		name     string
		lat, lon float64
		radius   int
	}{
		{name: "zero radius", lat: 1, lon: 2, radius: 0},
		{name: "too far", lat: 1, lon: 2, radius: MaxRadiusNM + 1},
		{name: "latitude", lat: -91, lon: 2, radius: 10},
		{name: "longitude", lat: 1, lon: 181, radius: 10},
		{name: "NaN", lat: 1, lon: math.NaN(), radius: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.GetAircraftWithinRange(context.Background(), tt.lat, tt.lon, tt.radius)
			require.Error(t, err)
			_, err = c.GetClosestAircraft(context.Background(), tt.lat, tt.lon, tt.radius)
			require.Error(t, err)
		})
	}
}

func TestGetClosestAircraft(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		c := server(t, "/v2/closest/-27.38420/153.11750/10", 200, `{"ac":[{"hex":"7c7a3b"}]}`)
		ac, err := c.GetClosestAircraft(context.Background(), -27.3842, 153.1175, 10)
		require.NoError(t, err)
		assert.Equal(t, "7c7a3b", ac.Hex)
	})

	t.Run("none", func(t *testing.T) {
		c := server(t, "/v2/closest/-27.38420/153.11750/10", 200, `{"ac":[]}`)
		_, err := c.GetClosestAircraft(context.Background(), -27.3842, 153.1175, 10)
		require.ErrorIs(t, err, ErrNotFound)
	})
}

func TestLookups(t *testing.T) {
	type lookup func(*Client, context.Context, string) ([]*Aircraft, error)

	tests := []struct {
		name    string
		fn      lookup
		code    string
		path    string
		errText string
	}{
		{name: "hex", fn: (*Client).GetAircraftByHex, code: "7C6B2D", path: "/v2/hex/7c6b2d"},
		{name: "callsign", fn: (*Client).GetAircraftByCallsign, code: " qfa615 ", path: "/v2/callsign/QFA615"},
		{name: "registration", fn: (*Client).GetAircraftByRegistration, code: "vh-vzs", path: "/v2/registration/VH-VZS"},
		{name: "type", fn: (*Client).GetAircraftByType, code: "b738", path: "/v2/type/B738"},
		{name: "escaped", fn: (*Client).GetAircraftByRegistration, code: "a/b", path: "/v2/registration/A%2FB"},
		{name: "squawk", fn: (*Client).GetAircraftBySquawk, code: "0123", path: "/v2/sqk/0123"},
		{name: "bad squawk", fn: (*Client).GetAircraftBySquawk, code: "7800", errText: "invalid squawk"},
		{name: "empty callsign", fn: (*Client).GetAircraftByCallsign, code: " ", errText: "a callsign is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := server(t, tt.path, 200, `{"ac":[{"hex":"7c6b2d"}]}`)

			ac, err := tt.fn(c, context.Background(), tt.code)

			if tt.errText != "" {
				require.ErrorContains(t, err, tt.errText)
				return
			}
			require.NoError(t, err)
			require.Len(t, ac, 1)
		})
	}
}

func TestLists(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*Client, context.Context) ([]*Aircraft, error)
		path string
	}{
		{name: "military", fn: (*Client).GetMilitaryAircraft, path: "/v2/mil"},
		{name: "LADD", fn: (*Client).GetLADDAircraft, path: "/v2/ladd"},
		{name: "PIA", fn: (*Client).GetPIAAircraft, path: "/v2/pia"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := server(t, tt.path, 200, `{"ac":[{"hex":"a"},{"hex":"b"}]}`)
			ac, err := tt.fn(c, context.Background())
			require.NoError(t, err)
			assert.Len(t, ac, 2)
		})
	}
}

func TestErrors(t *testing.T) {
	t.Run("rate limited", func(t *testing.T) {
		c := server(t, "/v2/mil", 429, "")
		_, err := c.GetMilitaryAircraft(context.Background())
		require.ErrorIs(t, err, ErrRateLimited)
	})

	t.Run("server error", func(t *testing.T) {
		c := server(t, "/v2/mil", 502, "bad gateway")
		_, err := c.GetMilitaryAircraft(context.Background())
		var se *adsbv2.StatusError
		require.ErrorAs(t, err, &se)
		assert.Equal(t, 502, se.StatusCode)
		assert.NotErrorIs(t, err, ErrRateLimited)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		c := server(t, "/v2/mil", 200, "not json")
		_, err := c.GetMilitaryAircraft(context.Background())
		require.ErrorContains(t, err, "unable to decode")
	})
}

func TestCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c, err := NewClient(nil, Config{URL: srv.URL})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = c.GetMilitaryAircraft(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
