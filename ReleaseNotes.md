# DHT Exporter - Release 1.0

First public release of DHT Exporter — a Prometheus exporter for DHT11/DHT22/AM2302 temperature and humidity sensors.

## About

DHT Exporter reads sensor data from the procfs interface created by the [DHT kernel driver](https://github.com/chapvic/dht-driver) and exposes it as Prometheus metrics on an HTTP endpoint. It also supports writing readings to a JSON file for integration with other monitoring systems.

## Highlights

- **Prometheus metrics**: temperature, humidity, status code, timestamp, and sensor info
- **JSON output**: structured sensor data with type and registration time (Unix timestamp)
- **Dynamic interval tracking**: automatically adjusts polling when the driver changes `auto_interval`
- **Driver availability tracking**: logs once when the driver disappears and once when it reappears — no log spam
- **Graceful shutdown**: clean ticker and HTTP server cleanup on SIGINT/SIGTERM
- **Security hardening**: runs as unprivileged user with `CAP_SYS_MODULE` under systemd
- **Static binary**: `CGO_ENABLED=0` with stripped symbols for minimal footprint

## Endpoints

| Endpoint | Description |
|----------|-------------|
| `/metrics` | Prometheus metrics |
| `/health` | Health check (200 OK) |

## Metrics

| Metric | Description |
|--------|-------------|
| `dht_temperature_celsius` | Temperature in Celsius |
| `dht_humidity_percent` | Relative humidity in percent |
| `dht_status_code` | Sensor status code (0 = success) |
| `dht_timestamp_seconds` | Unix timestamp of last measurement |
| `dht_info` | Sensor info (always 1) |

## Installation

```bash
make
sudo make install
sudo make start
```

## License

GNU General Public License v3. Copyright (c) 2026, Chapvic.
