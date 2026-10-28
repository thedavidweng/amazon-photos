# Build Your First Client with the Go SDK

This tutorial walks you through creating a small Go program that uses `github.com/thedavidweng/amazon-photos/pkg/aphotos` to inspect account usage, search photos by capture year, and upload a local file.

## Prerequisites

- Go 1.27.3 or later.
- Amazon Photos session cookies set in environment variables (`APHOTOS_SESSION_ID`, `APHOTOS_UBID`, `APHOTOS_AT`).

## 1. Initialize a Go Module

Create a new directory and initialize a module:

```sh
mkdir hello-aphotos
cd hello-aphotos
go mod init example.com/hello-aphotos
go get github.com/thedavidweng/amazon-photos@latest
```

## 2. Construct the Client

`aphotos.NewClient` validates configuration in memory without performing any network or disk I/O. Create `main.go`:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

func main() {
	client, err := aphotos.NewClient(aphotos.Options{
		Region: aphotos.RegionUS,
		Cookies: map[string]string{
			"session-id": os.Getenv("APHOTOS_SESSION_ID"),
			"ubid-main":  os.Getenv("APHOTOS_UBID"),
			"at-main":    os.Getenv("APHOTOS_AT"),
		},
	})
	if err != nil {
		log.Fatalf("create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	usage, err := client.GetUsage(ctx)
	if err != nil {
		log.Fatalf("get usage: %v", err)
	}
	fmt.Printf("Photos: %d items (%d bytes)\n", usage.Photo.Total.Count, usage.Photo.Total.Bytes)
	fmt.Printf("Videos: %d items (%d bytes)\n", usage.Video.Total.Count, usage.Video.Total.Bytes)

	page, err := client.Search(ctx, aphotos.SearchOptions{
		Filter: aphotos.NewFilter().MediaType(aphotos.MediaPhotos).Year(2025),
		Limit:  10,
		Sort:   aphotos.SortContentDateDesc,
	})
	if err != nil {
		log.Fatalf("search: %v", err)
	}

	for _, node := range page.Items {
		var captureDate time.Time
		if node.ContentProperties != nil {
			captureDate = node.ContentProperties.ContentDate
		}
		fmt.Printf("%s  %s  capture=%s  uploaded=%s\n",
			node.ID,
			node.Name,
			captureDate.Format(time.RFC3339),
			node.CreatedDate.Format(time.RFC3339),
		)
	}
}
```

## 3. Run the Program

Export your session cookies and run the program:

```sh
export APHOTOS_SESSION_ID="140-0000000-0000000"
export APHOTOS_UBID="130-0000000-0000000"
export APHOTOS_AT="Atza|..."
go run .
```

## 4. Test Your Code Offline with `http.RoundTripper`

Because `aphotos.Options` accepts a custom `Transport`, you can test your application without live Amazon credentials:

```go
client, err := aphotos.NewClient(aphotos.Options{
	Region: aphotos.RegionUS,
	Cookies: map[string]string{
		"session-id": "140-1111111-2222222",
		"ubid-main":  "130-3333333-4444444",
		"at-main":    "Atza|test-token",
	},
	Transport: myMockRoundTripper,
})
```

## Next Steps

- Explore all SDK types and methods in the [SDK API Reference](../reference/sdk-api.md).
- Learn how filter clauses and pagination work in [Search Filter DSL, Pagination, and Aggregations](../reference/search-filters-and-pagination.md).
- Read [Upload Confirmation, Retries, Idempotency, and Conflict Handling](../explanation/transfer-integrity-and-conflicts.md).
