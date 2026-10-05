# DHT Exporter - Release 1.1

Release date: 2026-10-05

## Overview

DHT Exporter 1.1 is a feature release that adds configuration file support,
driver interval write-back, and improved interval resolution. All changes
are backward compatible — existing deployments continue to work without
modification.

## What's New

### Configuration File Support

- New config file at `/etc/default/dht-exporter` with `PARAMS="..."` syntax
- Config parameters are parsed first; CLI arguments override them
- Startup banner reports config file status (found / not found)
- If the file does not exist, the program behaves exactly as before

### Driver Interval Write-Back

- When `--interval` is explicitly specified, the value is written back to
  the driver's `auto_interval` procfs file
- Startup banner reports interval source: `explicit` or `from driver`
- New function `writeAutoInterval()` handles the write-back

### Improved Interval Resolution

- Interval resolution moved from `pollLoop()` to `main()` (before the
  startup banner)
- If `--interval` is not specified, reads from the driver's `auto_interval`
- Dynamic interval tracking now updates the local variable when the driver
  changes `auto_interval` mid-run

### Other Improvements

- `registered` field in JSON output is now a Unix timestamp (`int64`)
  instead of an ISO 8601 string
- `forceFirstLog` flag replaces `prevSensorCount = -1` — fixes
  `sensors changed: -1 -> 1` on driver recovery
- `Type=exec` in systemd unit instead of `Type=notify`

## Backward Compatibility

- All existing CLI arguments and their behavior remain unchanged
- If `/etc/default/dht-exporter` does not exist, no config is loaded
- If `--interval` is not specified, the program reads the driver's
  `auto_interval` as before — no write-back occurs
- Existing JSON consumers should update to handle `registered` as integer

## Download

Download the source code and build with `make`, or use the pre-built
binary from the release assets.
