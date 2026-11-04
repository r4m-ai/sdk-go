# sdk-go

Official Go client for the [r4m.ai](https://r4m.ai) route optimization API: the
optimal driving order for a list of stops on real roads, with delivery windows,
service times and fleets, plus geocoding.

## Install

```bash
go get github.com/r4m-ai/sdk-go
```

## Optimize a route

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	r4m "github.com/r4m-ai/sdk-go"
)

func main() {
	ctx := context.Background()

	client, err := r4m.New(os.Getenv("R4M_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	// Submits, waits for the solver (usually 2–60 seconds) and returns the
	// finished plan. Addresses without Lat/Lng are geocoded in the same call.
	optimization, err := client.CreateOptimizationAndWaitForRoutes(ctx, r4m.CreateOptimizationRequest{
		Addresses: []r4m.Address{
			{Address: "Depot", Lat: r4m.Ptr(40.7128), Lng: r4m.Ptr(-74.006), IsDepot: r4m.Ptr(true)},
			{Address: "Stop 1", Lat: r4m.Ptr(40.758), Lng: r4m.Ptr(-73.9855)},
			{Address: "Stop 2", Lat: r4m.Ptr(40.7484), Lng: r4m.Ptr(-73.9857)},
		},
	}, r4m.WaitOptions{})
	if err != nil {
		log.Fatal(err)
	}

	for _, stop := range optimization.Result.Routes[0].Waypoints {
		fmt.Println(stop.Index)
	}
}
```

`Waypoints` is the driving order; each `Index` points back into the addresses
you sent. **Always check `Result.UnroutedStops`:** a stop listed there could not
be fitted and is not in the plan.

## Errors

Every call returns the value or an error. A refusal from the API is an
`*r4m.ApiError`; anything else never got an answer, such as a network failure.

```go
account, err := client.GetAccount(ctx)
if err != nil {
	var apiErr *r4m.ApiError
	if errors.As(err, &apiErr) {
		fmt.Println(apiErr.Code, apiErr.Status, apiErr.Message)
	}
	log.Fatal(err)
}
fmt.Println(account.PlanTier)
```

Waiting adds three more: `*r4m.OptimizationFailedError`,
`*r4m.OptimizationTimeoutError` and `*r4m.BusyError`.

Full reference: [r4m.ai/docs/sdks](https://r4m.ai/docs/sdks).

## License

MIT
