# First-Time CLI Setup, Authentication, Upload, and Verification

This tutorial walks you through installing `aphotos`, importing your Amazon Photos session cookies, running health checks, uploading a photo, and verifying the uploaded item in your library.

## Prerequisites

- An active Amazon Photos account.
- A desktop web browser signed in to Amazon Photos (`https://www.amazon.com/photos` or your regional marketplace).
- A local photo file (for example, `sample.jpg`).

## 1. Install `aphotos`

Install `aphotos` using Homebrew on macOS or Linux:

```sh
brew tap thedavidweng/homebrew-tap
brew install aphotos
```

Or install directly with Go 1.27.3+:

```sh
go install github.com/thedavidweng/amazon-photos/cmd/aphotos@latest
```

Verify the binary runs:

```sh
aphotos --version
```

## 2. Sign In Interactively

Run:

```sh
aphotos auth login
```

Choose your marketplace (for example `ca`). Follow the displayed Amazon Photos
link in your browser and finish Amazon sign-in, including MFA if requested.
Open Developer Tools → Network, reload Photos, and select an authenticated
request to your Amazon marketplace. Copy its **Cookie request header** value,
then paste it into the terminal and press Enter. Input is hidden; Ctrl+C cancels.
Do not copy `Set-Cookie` response headers.

The CLI checks the session with a read-only account request, then saves only the
three required session cookies to your selected profile with restricted file
permissions. A failed check leaves your previous profile intact. No OAuth grant
or password is requested by the CLI.

## 3. Verify Connectivity

For automation or existing exports, use `aphotos auth import <cookie-file>`;
see [sign-in instructions](../how-to/sign-in.md).

Check your authentication status:

```sh
aphotos auth status
```

Run the graded diagnostic check to verify local permissions, regional routing, account root resolution, and search connectivity:

```sh
aphotos doctor
```

Check your account storage usage:

```sh
aphotos usage
```

## 4. Plan and Upload a Photo

First, preview the upload with `--dry-run` (this performs zero network requests):

```sh
aphotos upload ./sample.jpg --dry-run
```

Now upload the file with conflict verification enabled:

```sh
aphotos upload ./sample.jpg --on-conflict skip --json
```

When the upload succeeds, `aphotos` emits a JSON envelope with `"ok": true`, `"confirmed": 1`, and the remote `nodeId` assigned by Amazon Photos.

## 5. Verify and Download the Uploaded Photo

Search for the uploaded file by name:

```sh
aphotos search --name sample.jpg --limit 5 --json
```

Download the file into a verification directory using its `nodeId`:

```sh
mkdir -p ./verified-download
aphotos download <NODE_ID> --out ./verified-download --json
```

`aphotos download` fetches the remote node's expected byte size and MD5 digest before streaming the file, verifies both on receipt, and writes the file atomically into `./verified-download/sample.jpg`.

## Next Steps

- Learn how to transfer entire directories in [Batch Upload and Download](../how-to/batch-transfers.md).
- Configure non-US marketplaces in [Configure Amazon Regions](../how-to/configure-regions.md).
- Browse all CLI flags in the [CLI Command Reference](../reference/cli-commands.md).
