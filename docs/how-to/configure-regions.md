# Configure Amazon Regions

This guide shows how to configure `aphotos` and the Go SDK for different Amazon marketplaces (`us`, `ca`, `uk`, `jp`, and European TLDs).

## Import Cookies for a Non-US Marketplace

Regional Amazon marketplaces use country-specific cookie suffixes (`ubid-acbca`/`at-acbca`, `ubid-acbuk`/`at-acbuk`, `ubid-acbjp`/`at-acbjp`, `ubid-acbde`/`at-acbde`, etc.).

When importing cookies, `aphotos` infers the region automatically if the cookie suffixes uniquely identify one marketplace, or you can pass `--region` explicitly:

```sh
aphotos auth import ./uk-cookies.json --region uk --profile uk-account
```

Verify the resolved endpoints and evidence status:

```sh
aphotos auth status --profile uk-account --json
```

## Switch Regions Per Command or via Environment Variables

Select a profile or override the region per command:

```sh
aphotos usage --profile uk-account
aphotos search --region de --profile eu-account --limit 20
```

Or set environment variables for stateless scripts:

```sh
export APHOTOS_REGION="jp"
export APHOTOS_SESSION_ID="140-0000000-0000000"
export APHOTOS_UBID="130-0000000-0000000"
export APHOTOS_AT="Atza|..."
aphotos usage --json
```

## Configure Regional Routing in the Go SDK

Pass `Region` in `aphotos.Options`:

```go
client, err := aphotos.NewClient(aphotos.Options{
	Region: aphotos.RegionUK,
	Cookies: map[string]string{
		"session-id": os.Getenv("APHOTOS_SESSION_ID"),
		"ubid-acbuk": os.Getenv("APHOTOS_UBID"),
		"at-acbuk":   os.Getenv("APHOTOS_AT"),
	},
})
```

If `Region` is omitted, `aphotos.NewClient` calls `InferRegionFromCookies`. If cookie suffixes are missing or mix multiple marketplaces (for example, both `ubid-main` and `ubid-acbuk`), `NewClient` fails fast with `*aphotos.ConfigError` rather than guessing `.com`.

See [Regions and Verification Status](../reference/regions.md) for the complete regional endpoint table.
