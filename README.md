# sdk-go

Official Go client for the [r4m.ai](https://r4m.ai) route optimization API.

This package is **auto-generated** from the r4m.ai OpenAPI spec with
[`oapi-codegen`](https://github.com/oapi-codegen/oapi-codegen). Do not edit by
hand — changes are overwritten on regeneration.

## Install

```bash
go get github.com/r4m-ai/sdk-go
```

## Usage

The bearer token is injected via a `RequestEditorFn`:

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	r4m "github.com/r4m-ai/sdk-go"
)

func main() {
	auth := func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+os.Getenv("R4M_API_KEY"))
		return nil
	}

	client, err := r4m.NewClientWithResponses(
		"https://r4m.ai",
		r4m.WithRequestEditorFn(auth),
	)
	if err != nil {
		panic(err)
	}

	resp, err := client.GetAccountWithResponse(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(string(resp.JSON200.Data.PlanTier))
}
```

## License

MIT
