# adsblol-client

[![Lint](https://github.com/aviation-tragic-au/adsblol-client/actions/workflows/lint.yml/badge.svg)](https://github.com/aviation-tragic-au/adsblol-client/actions/workflows/lint.yml) [![Test](https://github.com/aviation-tragic-au/adsblol-client/actions/workflows/test.yml/badge.svg)](https://github.com/aviation-tragic-au/adsblol-client/actions/workflows/test.yml)

A Go client for the [ADSB.lol](https://adsb.lol) API, a free, community-run
ADS-B aggregator. Aircraft come back as
[`adsbv2.Aircraft`](https://github.com/aviation-tragic-au/adsbv2), readsb's
v2 format, the same type as the other readsb-based clients.

ADSB.lol needs no key but rate limits by IP address. Please send a
User-Agent that identifies your program, and see
[ADSB.lol's API documentation](https://api.adsb.lol/docs) for its terms.

## Install

```sh
go get github.com/aviation-tragic-au/adsblol-client
```

## Usage

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log"

    adsblol "github.com/aviation-tragic-au/adsblol-client"
)

func main() {
    client, err := adsblol.NewClient(nil, adsblol.Config{
        UserAgent: "my-program/1.0 (+https://example.com)",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Everything within 25 nm of Brisbane Airport
    aircraft, err := client.GetAircraftWithinRange(context.Background(), -27.3842, 153.1175, 25)
    if errors.Is(err, adsblol.ErrRateLimited) {
        log.Fatal("rate limited; slow down")
    }
    if err != nil {
        log.Fatal(err)
    }

    for _, ac := range aircraft {
        feet, onGround := ac.Altitude()
        switch {
        case onGround:
            fmt.Printf("%s (%s) on the ground\n", ac.Callsign(), ac.Hex)
        case feet != nil:
            fmt.Printf("%s (%s) at %d ft\n", ac.Callsign(), ac.Hex, *feet)
        }
    }
}
```

### Methods

Every method takes a `context.Context` first.

| Method | Returns |
|---|---|
| `GetAircraftWithinRange(ctx, lat, lon, radiusNM)` | Every aircraft within 1 to 250 nm |
| `GetClosestAircraft(ctx, lat, lon, radiusNM)` | The nearest aircraft, or `ErrNotFound` |
| `GetAircraftByHex(ctx, hex)` | Aircraft with a 24-bit ICAO address |
| `GetAircraftByCallsign(ctx, callsign)` | Aircraft using a callsign |
| `GetAircraftByRegistration(ctx, registration)` | Aircraft with a registration |
| `GetAircraftByType(ctx, type)` | Aircraft of an ICAO type designator (`B738`) |
| `GetAircraftBySquawk(ctx, squawk)` | Aircraft squawking a code (four octal digits, as a string) |
| `GetMilitaryAircraft(ctx)` | Aircraft the database marks as military |
| `GetLADDAircraft(ctx)` / `GetPIAAircraft(ctx)` | Aircraft on the FAA's LADD list / using a privacy address |

A 429 matches `ErrRateLimited` (it's an `*adsbv2.RateLimitError` with the
`Retry-After` delay); any other non-200 is an `*adsbv2.StatusError`.

### Configuration

| Field        | Description |
|--------------|-------------|
| `UserAgent`  | Identifies your program (default `adsbv2.DefaultUserAgent`) |
| `URL`        | API base (default `https://api.adsb.lol`) |
| `HTTPClient` | Your own `*http.Client` (the default times out after 10 seconds) |

`Config` carries `json` and `mapstructure` tags, so it can be decoded
straight from a config file or Viper.

## License

Copyright 2026 Dean Stalker

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
