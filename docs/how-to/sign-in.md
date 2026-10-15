# Sign In or Refresh an Expired Session

1. Run `aphotos auth login` in an interactive terminal. To skip the region prompt,
   use `aphotos auth login --region ca` (or your marketplace code).
2. Open the displayed Amazon Photos URL in your browser and complete Amazon's
   password/MFA steps.
3. In Developer Tools → Network, reload Photos and select an authenticated
   request. Copy its **Cookie request header** value.
4. Paste at the hidden Cookie prompt and press Enter. Do not put cookies on the
   command line, in chat, or in a screenshot. Ctrl+C cancels without saving.
5. Wait for the read-only check, then run `aphotos usage`.

401 means the session must be refreshed; sign in again on the same marketplace.
403 means this account is not allowed to access the requested API. 429, timeout,
or offline errors are temporary: reconnect/wait and run login again. Your previous
profile is preserved on failure. Successful login is evidence of account read
access, not verified uploads or a change to a region's protocol evidence grade.

For scripts, export a raw Cookie header, JSON, or Netscape cookie file with
restricted permissions and run `aphotos auth import ./cookies.json --region ca`.
Unlike interactive login, import validates configuration without a live login
check; use `aphotos doctor` afterward. See [CLI reference](../reference/cli-commands.md).
