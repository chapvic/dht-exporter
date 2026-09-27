/*
 * dht-exporter.go - Prometheus Exporter for DHT11/DHT22/AM2302 Sensors
 *
 * A Prometheus exporter that reads temperature and humidity data from the
 * procfs interface created by the dht kernel driver at /proc/sensors/dht/.
 *
 * The exporter polls sensor data from /proc/sensors/dht/gpio<pin>/ and exposes
 * it as Prometheus metrics on an HTTP endpoint. It also supports writing
 * readings to a JSON file for integration with other monitoring systems.
 *
 * Copyright (c) 2026, Chapvic
 *
 * Version: 1.0
 *
 * License: GPLv3
 *
 * Features:
 *   - Reads sensor data from /proc/sensors/dht/gpio<pin>/value
 *   - Exposes Prometheus metrics on configurable HTTP endpoint
 *   - Optional JSON file output for integration with other tools
 *   - Dynamic interval tracking: monitors driver's auto_interval
 *     and adjusts polling automatically when the driver changes it
 *   - Graceful shutdown on SIGINT/SIGTERM with clean ticker cleanup
 *   - ANSI color logging in terminal, plain text in redirected output
 *   - Sensor name sanitization with hostname fallback
 *   - HTTP read/write timeouts for resource protection
 *   - Immediate first poll on startup, then periodic by interval
 *   - Logs sensor readings on first poll and when count changes
 *   - JSON output with structured sensor info (type + registration time)
 *   - Driver availability tracking: logs once when driver disappears
 *     and once when it reappears, avoiding log spam
 *
 * Options:
 *   --name <name>      Exporter name label (default: hostname)
 *   --addr <addr>      HTTP listen address (default: 0.0.0.0:9988)
 *   --interval <sec>   Poll interval 2-60, 0 = use driver value (default: 10)
 *   --json <path>      Write JSON output to file (default: disabled)
 *   --driver <name>    Kernel module name for modprobe (default: dht)
 *   --rt <sec>         HTTP read timeout 1-60 (default: 5)
 *   --wt <sec>         HTTP write timeout 1-120, >= --rt (default: 10)
 *   --help             Show help message
 *
 * Endpoints:
 *   /metrics           Prometheus metrics
 *   /health            Health check (200 OK)
 *
 * Prometheus metrics:
 *   dht_temperature_celsius   Temperature in Celsius
 *   dht_humidity_percent      Relative humidity in percent
 *   dht_status_code           Sensor status code (0 = success)
 *   dht_timestamp_seconds     Unix timestamp of last measurement
 *   dht_info                  Sensor info (always 1)
 */

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

/* Version */
const version = "1.0"

/* Defaults and limits */
const (
	defaultAddr      = "0.0.0.0:9988"
	defaultInterval  = 10
	defaultReadTO    = 5
	defaultWriteTO   = 10
	defaultDriver    = "dht"
	minInterval      = 2
	maxInterval      = 60
	minReadTO        = 1
	maxReadTO        = 60
	minWriteTO       = 1
	maxWriteTO       = 120
	procBaseDir      = "/proc/sensors/dht"
	autoIntervalFile = "/proc/sensors/dht/auto_interval"
)

/* Logging */
var useColor bool

func init() {
	/*
	 * Detect whether stderr is a terminal (for ANSI color codes).
	 * When output is redirected to a file or pipe, colors are disabled
	 * to keep logs clean and greppable.
	 */
	fi, err := os.Stderr.Stat()
	if err != nil {
		useColor = false
		return
	}
	useColor = (fi.Mode() & os.ModeCharDevice) != 0
}

/* ANSI color codes */
const (
	cReset  = "\033[0m"
	cRed    = "\033[31m"
	cYellow = "\033[33m"
	cCyan   = "\033[36m"
)

func logInfo(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().Format("2006/01/02 15:04:05")
	if useColor {
		fmt.Fprintf(os.Stderr, "%s %s[INFO]%s %s\n", ts, cCyan, cReset, msg)
	} else {
		fmt.Fprintf(os.Stderr, "%s [INFO] %s\n", ts, msg)
	}
}

func logWarn(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().Format("2006/01/02 15:04:05")
	if useColor {
		fmt.Fprintf(os.Stderr, "%s %s[WARN]%s %s\n", ts, cYellow, cReset, msg)
	} else {
		fmt.Fprintf(os.Stderr, "%s [WARN] %s\n", ts, msg)
	}
}

func logError(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().Format("2006/01/02 15:04:05")
	if useColor {
		fmt.Fprintf(os.Stderr, "%s %s[ERROR]%s %s\n", ts, cRed, cReset, msg)
	} else {
		fmt.Fprintf(os.Stderr, "%s [ERROR] %s\n", ts, msg)
	}
}

/* Prometheus metrics */
var (
	metricTemperature = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dht_temperature_celsius",
			Help: "Temperature in Celsius",
		},
		[]string{"name", "pin"},
	)
	metricHumidity = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dht_humidity_percent",
			Help: "Relative humidity in percent",
		},
		[]string{"name", "pin"},
	)
	metricStatusCode = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dht_status_code",
			Help: "Sensor status code (0 = success)",
		},
		[]string{"name", "pin"},
	)
	metricTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dht_timestamp_seconds",
			Help: "Unix timestamp of last measurement",
		},
		[]string{"name", "pin"},
	)
	metricInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dht_info",
			Help: "Sensor info (always 1)",
		},
		[]string{"name", "pin", "type", "status"},
	)
)

/* Data structures */

/*
 * sensorReading holds parsed data from a single sensor's procfs entry.
 * StatusText is the human-readable status from /proc/sensors/dht/gpio<pin>/status_text.
 */
type sensorReading struct {
	Pin         int
	Humidity    float64
	Temperature float64
	StatusCode  int
	Timestamp   int64
	Info        string
	StatusText  string
}

/* exporter holds all runtime configuration */
type exporter struct {
	name         string
	addr         string
	interval     int
	jsonPath     string
	driverName   string
	readTimeout  int
	writeTimeout int
}

/*
 * sanitizeName strips all characters except [A-Za-z0-9._-] from the name.
 * If the result is empty, falls back to the system hostname.
 * If hostname is also empty, uses "localhost".
 */
func sanitizeName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '.' || r == '_' {
			sb.WriteRune(r)
		}
	}
	result := sb.String()
	if result == "" {
		hostname, err := os.Hostname()
		if err != nil || hostname == "" {
			hostname = "localhost"
		}
		result = hostname
	}
	return result
}

/*
 * validateJSONPath warns if the JSON output path is outside typical
 * directories. Does not block — if the path is writable, the file
 * will be created regardless.
 */
func validateJSONPath(path string) {
	if path == "" {
		return
	}
	typical := []string{"/etc/", "/opt/", "/run/", "/tmp/", "/var/lib/", "/var/log/", "/usr/local/etc/"}
	for _, prefix := range typical {
		if strings.HasPrefix(path, prefix) {
			return
		}
	}
	logWarn("JSON path '%s' is outside typical directories (/etc/, /opt/, /run/, /tmp/, /var/lib/, /var/log/, /usr/local/etc/)", path)
}

/*
 * readAutoInterval reads the driver's global auto_interval value.
 * Returns -1 if the file is unavailable or the value cannot be parsed.
 * -1 means auto-poll is disabled in the driver.
 */
func readAutoInterval() int {
	data, err := os.ReadFile(autoIntervalFile)
	if err != nil {
		return -1
	}
	val, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return -1
	}
	return val
}

/*
 * ensureDriver attempts to load the kernel module via modprobe if the
 * procfs interface is not found. Returns nil on success or if the
 * interface already exists. On modprobe failure, logs a warning and
 * continues — the driver may be loaded later or manually.
 */
func ensureDriver(driverName string) {
	// Check if procfs interface already exists
	if _, err := os.Stat(procBaseDir); err == nil {
		return
	}

	logInfo("driver path %s/ not found, attempting modprobe %s", procBaseDir, driverName)
	cmd := exec.Command("modprobe", driverName)
	if err := cmd.Run(); err != nil {
		logWarn("modprobe %s failed: %v — driver may need to be loaded manually", driverName, err)
		return
	}
	logInfo("modprobe %s completed", driverName)
}

/*
 * readSensors reads all sensor data from the procfs interface.
 * Returns (readings, error):
 *   - error = os.ErrNotExist: driver not loaded (procfs dir missing)
 *   - error = other: I/O or parsing error reading the directory
 *   - error = nil, readings empty: driver loaded but no sensors registered
 *   - error = nil, readings non-empty: sensors found and parsed
 */
func readSensors() ([]sensorReading, error) {
	entries, err := os.ReadDir(procBaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}

	var readings []sensorReading
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "gpio") {
			continue
		}
		pinStr := strings.TrimPrefix(name, "gpio")
		pin, err := strconv.Atoi(pinStr)
		if err != nil {
			continue
		}

		sensorDir := procBaseDir + "/" + name

		// Read value file: "H=<humidity>\nT=<temperature>\n"
		valData, err := os.ReadFile(sensorDir + "/value")
		if err != nil {
			logWarn("cannot read value for pin %d: %v", pin, err)
			continue
		}

		humidity, temperature, ok := parseValueFile(string(valData))
		if !ok {
			logWarn("cannot parse value for pin %d: %s", pin, strings.TrimSpace(string(valData)))
			continue
		}

		// Read status code
		statusCode := 0
		scData, err := os.ReadFile(sensorDir + "/status_code")
		if err == nil {
			if sc, err := strconv.Atoi(strings.TrimSpace(string(scData))); err == nil {
				statusCode = sc
			}
		}

		// Read status text
		statusText := ""
		stData, err := os.ReadFile(sensorDir + "/status_text")
		if err == nil {
			statusText = strings.TrimSpace(string(stData))
		}

		// Read timestamp
		var timestamp int64
		tsData, err := os.ReadFile(sensorDir + "/timestamp")
		if err == nil {
			if ts, err := strconv.ParseInt(strings.TrimSpace(string(tsData)), 10, 64); err == nil {
				timestamp = ts
			}
		}

		// Read info
		info := ""
		infoData, err := os.ReadFile(sensorDir + "/info")
		if err == nil {
			info = strings.TrimSpace(string(infoData))
		}

		readings = append(readings, sensorReading{
			Pin:         pin,
			Humidity:    humidity,
			Temperature: temperature,
			StatusCode:  statusCode,
			Timestamp:   timestamp,
			Info:        info,
			StatusText:  statusText,
		})
	}

	return readings, nil
}

/*
 * parseValueFile parses the "H=<humidity>\nT=<temperature>\n" format.
 * Both T= and T=- prefixes are checked for robustness.
 * Returns (humidity, temperature, ok).
 */
func parseValueFile(data string) (float64, float64, bool) {
	var humidity, temperature float64
	var hFound, tFound bool

	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "H=") {
			val, err := strconv.ParseFloat(strings.TrimPrefix(line, "H="), 64)
			if err == nil {
				humidity = val
				hFound = true
			}
		}
		// Check both T= and T=- for robustness (redundant but harmless)
		if strings.HasPrefix(line, "T=") || strings.HasPrefix(line, "T=-") {
			val, err := strconv.ParseFloat(strings.TrimPrefix(line, "T="), 64)
			if err == nil {
				temperature = val
				tFound = true
			}
		}
	}

	return humidity, temperature, hFound && tFound
}

/*
 * updateMetrics updates Prometheus metrics for all sensors.
 * Instead of Reset(), uses DeleteLabelValues for pins that have
 * disappeared since the last poll, preserving live sensor metrics.
 */
func updateMetrics(readings []sensorReading, exporterName string, prevPins map[int]bool) map[int]bool {
	currentPins := make(map[int]bool)
	for _, r := range readings {
		labels := prometheus.Labels{
			"name": exporterName,
			"pin":  strconv.Itoa(r.Pin),
		}

		metricTemperature.With(labels).Set(r.Temperature)
		metricHumidity.With(labels).Set(r.Humidity)
		metricStatusCode.With(labels).Set(float64(r.StatusCode))
		metricTimestamp.With(labels).Set(float64(r.Timestamp))

		// Extract sensor type from info string if available
		sensorType := "unknown"
		if strings.Contains(strings.ToLower(r.Info), "dht22") || strings.Contains(strings.ToLower(r.Info), "am2302") {
			sensorType = "dht22"
		} else if strings.Contains(strings.ToLower(r.Info), "dht11") {
			sensorType = "dht11"
		}

		statusText := "success"
		if r.StatusCode != 0 {
			statusText = "error"
		}

		infoLabels := prometheus.Labels{
			"name":   exporterName,
			"pin":    strconv.Itoa(r.Pin),
			"type":   sensorType,
			"status": statusText,
		}
		metricInfo.With(infoLabels).Set(1)

		currentPins[r.Pin] = true
	}

	// Delete metrics for pins that existed last poll but are gone now
	for pin := range prevPins {
		if !currentPins[pin] {
			pinStr := strconv.Itoa(pin)
			baseLabels := prometheus.Labels{
				"name": exporterName,
				"pin":  pinStr,
			}
			metricTemperature.Delete(baseLabels)
			metricHumidity.Delete(baseLabels)
			metricStatusCode.Delete(baseLabels)
			metricTimestamp.Delete(baseLabels)
			/*
			 * For metricInfo we need to try deleting with known type/status combos.
			 * Since we don't track them, we use DeletePartialMatch.
			 */
			metricInfo.DeletePartialMatch(prometheus.Labels{
				"name": exporterName,
				"pin":  pinStr,
			})
		}
	}

	return currentPins
}

/*
 * jsonSensorInfo is the structured representation of the raw info string
 * from /proc/sensors/dht/gpio<pin>/info. The raw string has the format:
 *   "Sensor type: DHT22\nRegister time: 2026-09-27T13:16:09Z"
 * This struct splits it into "sensor" and "registered" fields.
 */
type jsonSensorInfo struct {
	Sensor     string `json:"sensor"`
	Registered int64  `json:"registered"`
}

/*
 * parseInfo parses the raw info string from procfs into a structured object.
 * The info string has the format:
 *   "Sensor type: DHT22\nRegister time: 2026-09-27T13:16:09Z"
 * Returns a jsonSensorInfo struct with "sensor" and "registered" fields.
 * If the format is unexpected, returns the raw string in "sensor" and
 * leaves "registered" empty.
 */
func parseInfo(rawInfo string) jsonSensorInfo {
	sensor := ""
	registeredStr := ""

	for _, line := range strings.Split(rawInfo, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Sensor type:") {
			sensor = strings.TrimSpace(strings.TrimPrefix(line, "Sensor type:"))
		} else if strings.HasPrefix(line, "Register time:") {
			registeredStr = strings.TrimSpace(strings.TrimPrefix(line, "Register time:"))
		}
	}

	/* Convert ISO 8601 registration time to Unix timestamp */
	var registered int64
	if registeredStr != "" {
		if t, err := time.Parse(time.RFC3339, registeredStr); err == nil {
			registered = t.Unix()
		}
	}

	/* If we could not parse structured fields, fall back to raw string */
	if sensor == "" && registered == 0 {
		sensor = rawInfo
	}

	return jsonSensorInfo{
		Sensor:     sensor,
		Registered: registered,
	}
}

/* writeJSON writes sensor readings to a JSON file */
func writeJSON(readings []sensorReading, path string) {
	type jsonSensor struct {
		Pin         int            `json:"pin"`
		Humidity    float64        `json:"humidity"`
		Temperature float64        `json:"temperature"`
		StatusCode  int            `json:"status_code"`
		Timestamp   int64          `json:"timestamp"`
		Info        jsonSensorInfo `json:"info"`
		StatusText  string         `json:"status_text"`
	}

	type jsonOutput struct {
		Timestamp int64        `json:"timestamp"`
		Sensors   []jsonSensor `json:"sensors"`
	}

	var sensors []jsonSensor
	for _, r := range readings {
		sensors = append(sensors, jsonSensor{
			Pin:         r.Pin,
			Humidity:    r.Humidity,
			Temperature: r.Temperature,
			StatusCode:  r.StatusCode,
			Timestamp:   r.Timestamp,
			Info:        parseInfo(r.Info),
			StatusText:  r.StatusText,
		})
	}

	output := jsonOutput{
		Timestamp: time.Now().Unix(),
		Sensors:   sensors,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		logError("JSON marshal failed: %v", err)
		return
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		logError("cannot write JSON file '%s': %v", path, err)
	}
}

/*
 * logSensorReadings logs sensor readings to stderr.
 * On first poll: "first reading: N sensor(s) registered".
 * On count change: "sensors changed: M -> N".
 * Individual sensor lines are always printed when logging.
 */
func logSensorReadings(readings []sensorReading, prevCount int, isFirst bool) {
	if isFirst {
		logInfo("first reading: %d sensor(s) registered", len(readings))
	} else {
		logInfo("sensors changed: %d -> %d", prevCount, len(readings))
	}
	for _, r := range readings {
		status := r.StatusText
		if status == "" {
			if r.StatusCode == 0 {
				status = "SUCCESS"
			} else {
				status = fmt.Sprintf("ERROR(%d)", r.StatusCode)
			}
		}
		logInfo("gpio%d: T=%.1f C, H=%.1f%%, status: %s", r.Pin, r.Temperature, r.Humidity, status)
	}
}

/*
 * pollLoop runs the sensor polling loop. It continuously monitors the
 * driver's auto_interval and adjusts the ticker if the driver changes it.
 * The first poll happens immediately on startup, before the first tick.
 * Driver availability is tracked: when the driver disappears, a single
 * WARN is logged; when it reappears, a single INFO is logged followed
 * by sensor readings as on startup.
 * Stops when the stopCh channel is closed.
 */
func pollLoop(e *exporter, stopCh <-chan struct{}) {
	// Validate interval: must be 2-60, 0 means "use driver value"
	interval := e.interval
	if interval < minInterval || interval > maxInterval {
		if interval != 0 {
			logWarn("interval %d is outside valid range %d-%d, falling back to driver value", interval, minInterval, maxInterval)
		}
		interval = 0
	}

	// If interval is 0, try to read from driver
	if interval == 0 {
		driverInterval := readAutoInterval()
		if driverInterval >= minInterval && driverInterval <= maxInterval {
			interval = driverInterval
		} else {
			interval = defaultInterval
		}
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	prevPins := make(map[int]bool)
	prevSensorCount := -1
	lastDriverInterval := readAutoInterval()

	// Track driver availability to avoid log spam
	driverAvailable := true
	forceFirstLog := false

	// Immediate first poll before ticker
	readings, err := readSensors()
	if err != nil {
		if os.IsNotExist(err) {
			logWarn("DHT driver not loaded: %s/ not found", procBaseDir)
			driverAvailable = false
		} else {
			logWarn("cannot read sensors: %v", err)
		}
	} else {
		prevPins = updateMetrics(readings, e.name, prevPins)
		prevSensorCount = len(readings)
		logSensorReadings(readings, 0, true)
		if e.jsonPath != "" {
			writeJSON(readings, e.jsonPath)
		}
	}

	for {
		select {
		case <-stopCh:
			logInfo("poll loop stopped")
			return
		case <-ticker.C:
			/*
			 * Dynamic interval tracking: on each tick, re-read the driver's
			 * auto_interval. If it has changed, adjust the ticker and log.
			 */
			driverInterval := readAutoInterval()
			if driverInterval >= minInterval && driverInterval <= maxInterval {
				if driverInterval != lastDriverInterval && lastDriverInterval >= 0 {
					logInfo("driver interval changed: %d -> %d, adjusting", lastDriverInterval, driverInterval)
					ticker.Reset(time.Duration(driverInterval) * time.Second)
				}
				lastDriverInterval = driverInterval
			}

			// Read sensors
			readings, err := readSensors()
			if err != nil {
				if os.IsNotExist(err) {
					// Log only once when driver disappears
					if driverAvailable {
						logWarn("DHT driver not loaded: %s/ not found", procBaseDir)
						driverAvailable = false
					}
				} else {
					logWarn("cannot read sensors: %v", err)
				}
				// Don't touch metrics on errors — preserve last known values
				continue
			}

			// Driver is available again
			if !driverAvailable {
				logInfo("DHT driver ready: %s", procBaseDir)
				driverAvailable = true
				// Force sensor readings log like on startup
				forceFirstLog = true
			}

			// Update metrics
			prevPins = updateMetrics(readings, e.name, prevPins)

			// Log on first poll, count change, or driver recovery
			if forceFirstLog {
				logSensorReadings(readings, 0, true)
				forceFirstLog = false
			} else if prevSensorCount != len(readings) {
				logSensorReadings(readings, prevSensorCount, false)
			}
			prevSensorCount = len(readings)

			// Write JSON if configured
			if e.jsonPath != "" {
				writeJSON(readings, e.jsonPath)
			}
		}
	}
}

/* printHelp prints the help message in the exact format specified */
func printHelp() {
	fmt.Fprintf(os.Stderr, `DHT Exporter, version %s

A Prometheus exporter for DHT11/DHT22/AM2302 temperature and humidity sensors.
Reads sensor data from the procfs interface created by the dht kernel driver
at /proc/sensors/dht/gpio<pin>/.

Copyright (c) 2026, Chapvic

Usage:
  dht-exporter [options]

Options:
  --name <name>        Exporter name used as a Prometheus label.
                       Special characters are stripped; if empty or becomes
                       empty after sanitization, the system hostname is used.
                       (default: hostname)

  --addr <addr>        HTTP listen address.
                       (default: 0.0.0.0:9988)

  --interval <sec>     Polling interval in seconds.
                       Valid range: 2-60. Values outside this range produce
                       a warning and fall back to the driver's auto_interval.
                       If the driver is unavailable, defaults to 10.
                       The exporter continuously monitors the driver's
                       auto_interval and adjusts automatically if it changes.
                       (default: 10)

  --json <path>        Write sensor readings to a JSON file in addition to
                       Prometheus metrics. The file is updated on each poll.
                       Typical directories: /etc/, /opt/, /run/, /tmp/,
                       /var/lib/, /var/log/, /usr/local/etc/.

  --driver <name>      Kernel module name for modprobe if /proc/sensors/dht/
                       is not found at startup. Useful for driver forks.
                       (default: dht)

  --rt <sec>           HTTP read timeout in seconds.
                       Range: 1-60. (default: 5)

  --wt <sec>           HTTP write timeout in seconds.
                       Range: 1-120. Must be >= --rt. (default: 10)

  --help               Show this help message.

Prometheus metrics:
  dht_temperature_celsius  Temperature in Celsius
  dht_humidity_percent     Relative humidity in percent
  dht_status_code          Sensor status code (0 = success)
  dht_timestamp_seconds    Unix timestamp of last measurement
  dht_info                 Sensor info (always 1)

Endpoints:
  /metrics    Prometheus metrics
  /health     Health check (returns 200 OK)

Environment:
  Requires the dht kernel driver loaded with procfs interface at
  /proc/sensors/dht/. Register sensors by writing BCM pin numbers to
  /proc/sensors/dht/export.

License: GNU GPLv3
`, version)
}

/* main */
func main() {
	/* Parse command-line arguments */
	e := &exporter{
		addr:         defaultAddr,
		interval:     defaultInterval,
		driverName:   defaultDriver,
		readTimeout:  defaultReadTO,
		writeTimeout: defaultWriteTO,
	}

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--help":
			printHelp()
			os.Exit(0)
		case "--name":
			if i+1 < len(args) {
				e.name = args[i+1]
				i++
			}
		case "--addr":
			if i+1 < len(args) {
				e.addr = args[i+1]
				i++
			}
		case "--interval":
			if i+1 < len(args) {
				val, err := strconv.Atoi(args[i+1])
				if err != nil {
					logError("invalid --interval value: %s", args[i+1])
					os.Exit(1)
				}
				e.interval = val
				i++
			}
		case "--json":
			if i+1 < len(args) {
				e.jsonPath = args[i+1]
				i++
			}
		case "--driver":
			if i+1 < len(args) {
				e.driverName = args[i+1]
				i++
			}
		case "--rt":
			if i+1 < len(args) {
				val, err := strconv.Atoi(args[i+1])
				if err != nil {
					logError("invalid --rt value: %s", args[i+1])
					os.Exit(1)
				}
				e.readTimeout = val
				i++
			}
		case "--wt":
			if i+1 < len(args) {
				val, err := strconv.Atoi(args[i+1])
				if err != nil {
					logError("invalid --wt value: %s", args[i+1])
					os.Exit(1)
				}
				e.writeTimeout = val
				i++
			}
		default:
			if strings.HasPrefix(arg, "--") {
				logError("unknown option: %s", arg)
				printHelp()
				os.Exit(1)
			}
		}
	}

	/* Validate read/write timeouts */
	if e.readTimeout < minReadTO || e.readTimeout > maxReadTO {
		logError("--rt must be between %d and %d (got %d)", minReadTO, maxReadTO, e.readTimeout)
		os.Exit(1)
	}
	if e.writeTimeout < minWriteTO || e.writeTimeout > maxWriteTO {
		logError("--wt must be between %d and %d (got %d)", minWriteTO, maxWriteTO, e.writeTimeout)
		os.Exit(1)
	}
	if e.writeTimeout < e.readTimeout {
		logError("--wt (%d) must be >= --rt (%d)", e.writeTimeout, e.readTimeout)
		os.Exit(1)
	}

	/* Sanitize name */
	e.name = sanitizeName(e.name)

	/* Register Prometheus metrics */
	prometheus.MustRegister(
		metricTemperature,
		metricHumidity,
		metricStatusCode,
		metricTimestamp,
		metricInfo,
	)

	/* Attempt to load driver if needed */
	ensureDriver(e.driverName)

	/* Print startup banner */
	logInfo("DHT Exporter (v%s) starting...", version)
	logInfo("  name:      %s", e.name)
	logInfo("  address:   %s", e.addr)
	logInfo("  interval:  %d seconds", e.interval)
	logInfo("  driver:    %s", e.driverName)
	if e.jsonPath != "" {
		logInfo("  json:      %s", e.jsonPath)
	} else {
		logInfo("  json:      disabled (no --json specified)")
	}
	logInfo("  timeouts:  read=%ds, write=%ds", e.readTimeout, e.writeTimeout)
	logInfo("  procfs:    %s", procBaseDir)

	/* Validate JSON path after banner */
	if e.jsonPath != "" {
		validateJSONPath(e.jsonPath)
	}

	logInfo("DHT Exporter ready")

	/* Set up HTTP server */
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	})

	server := &http.Server{
		Addr:         e.addr,
		Handler:      mux,
		ReadTimeout:  time.Duration(e.readTimeout) * time.Second,
		WriteTimeout: time.Duration(e.writeTimeout) * time.Second,
	}

	/*
	 * Signal handling: use a channel for signals and a channel to stop
	 * the poll loop. The main goroutine blocks on select, waiting for
	 * either a signal or an HTTP server error. On either event, it
	 * shuts down cleanly.
	 */
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	stopCh := make(chan struct{})

	// Start poll loop in a goroutine
	go pollLoop(e, stopCh)

	// Start HTTP server in a goroutine
	errCh := make(chan error, 1)
	go func() {
		logInfo("HTTP server listening on %s", e.addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	// Wait for signal or server error
	select {
	case sig := <-sigCh:
		logInfo("received signal %v, shutting down...", sig)
	case err, ok := <-errCh:
		if ok && err != nil {
			logError("HTTP server error: %v", err)
		}
	}

	/* Graceful shutdown */
	// Stop the poll loop
	close(stopCh)

	// Shutdown HTTP server with a 5-second timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logError("HTTP server shutdown error: %v", err)
	}

	logInfo("shutdown complete")
}
