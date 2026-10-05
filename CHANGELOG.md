# Changelog

All notable changes to the Go version of `dht-exporter` are documented here.

## Added

### Configuration file support (`/etc/default/dht-exporter`)

- New constant `configFile = "/etc/default/dht-exporter"`.
- New function `readConfigParams() ([]string, bool)` — reads and parses the
  config file:
  1. Opens the config file. If the file does not exist, returns `(nil, false)`.
  2. Reads line by line, skipping empty lines and `#`-comments.
  3. Looks for a line starting with `PARAMS=`.
  4. Strips surrounding single or double quotes from the value.
  5. Tokenizes the value shell-style: quoted tokens (`"..."` or `'...'`) are
     read until the closing quote; unquoted tokens until whitespace.
  6. Returns the token slice and `true`.
- In `main()`, config parameters are prepended to CLI arguments, producing
  a merged list: `configParams + cliArgs`. Since the argument parser processes
  the list sequentially (last wins), CLI arguments override the config file —
  identical to the C version's `new_argv = [argv[0]] + config_params + cli_args`.
- Startup banner now reports config file status:
  `config: /etc/default/dht-exporter (found)` or `(not found)`.
- Help message updated with a new "Config file" section describing the file
  path and the precedence rules (config first, CLI override).

### Write-back to driver (`auto_interval`)

- New field `intervalExplicit bool` on the `exporter` struct — tracks whether
  `--interval` was specified explicitly by the user.
- New function `writeAutoInterval(interval int) error` — writes the integer
  interval as a string to `/proc/sensors/dht/auto_interval`. On failure, logs
  a warning and returns the error (non-fatal).
- When `--interval` is parsed, `intervalExplicit` is set to `true`.
- Interval resolution moved from `pollLoop()` to `main()` (before the startup
  banner):
  - If `intervalExplicit` is `true` and the value is within `[2, 60]`, the
    value is kept as-is.
  - If `intervalExplicit` is `true` but the value is outside `[2, 60]`, a
    warning is logged, the interval falls back to `defaultInterval`, and
    `intervalExplicit` is reset to `false`.
  - If `intervalExplicit` is `false`, the interval is read from the driver
    via `readAutoInterval()`. If the driver value is within range, it is used;
    otherwise `defaultInterval` is used.
- In `pollLoop()`, if `intervalExplicit` is `true`, `writeAutoInterval()` is
  called at startup to push the user-specified interval back into the driver.
  Success is logged as `driver interval set to N seconds`.
- Startup banner now reports the interval source:
  `interval: 15 seconds (explicit)` or `interval: 10 seconds (from driver)`.
- Dynamic interval tracking in `pollLoop()` now also updates the local
  `interval` variable when the driver changes `auto_interval` mid-run
  (previously only `ticker.Reset` was called without updating the variable).

## Changed

- Help message: `--interval` description updated to note that an explicitly
  specified value is written back to the driver.
- File header comment updated to mention config file support and write-back.

## Backward compatibility

- All existing CLI arguments and their behavior remain unchanged.
- If `/etc/default/dht-exporter` does not exist, the program behaves exactly
  as before — no config is loaded, no warning is emitted.
- If `--interval` is not specified, the program reads the driver's
  `auto_interval` as before — no write-back occurs.
