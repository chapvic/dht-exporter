# =========================================================================
# Makefile for DHT Exporter
# =========================================================================
#
# Commands:
#   make             Build the binary
#   make install     Install binary, systemd unit, create user, enable service
#   make uninstall   Stop service, remove binary, unit file, and user
#   make start       Start the service
#   make stop        Stop the service
#   make restart     Restart the service
#   make status      Show service status
#   make logs        Follow service logs (journalctl -f)
#   make clean       Remove build artifacts
#
# The build uses CGO_ENABLED=0 for a static binary with no C dependencies,
# and -ldflags="-s -w" to strip debug symbols and DWARF tables for a
# smaller binary size.
# =========================================================================

# --- Build configuration ---
BINARY   = dht-exporter
SRC      = dht-exporter.go
UNIT     = dht-exporter.service
CONF     = dht-exporter.default

# Installation paths
PREFIX   = /usr/local
BIN_DIR  = $(PREFIX)/bin
UNIT_DIR = /etc/systemd/system
CONF_DIR = /etc/default

# Service user (created during install, removed during uninstall)
USER     = dht-exporter

# --- Phony targets (do not represent files) ---
.PHONY: all install uninstall start stop restart status logs clean

# =========================================================================
# Build target
# =========================================================================

# Default target: build the binary
all: $(BINARY)

# Build the Go binary with CGO disabled (static linking) and stripped symbols.
# Initializes go module if not already present, tidies dependencies,
# then builds with CGO_ENABLED=0 for a static binary.
# -s: strip symbol table
# -w: strip DWARF debug info
$(BINARY): $(SRC)
	@if [ ! -f go.mod ]; then go mod init $(BINARY); fi
	go mod tidy
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) $(SRC)

# =========================================================================
# Install / Uninstall
# =========================================================================

# Install: copy binary and unit file, create dedicated user, enable service.
# This target should be run as root (e.g., via sudo make install).
install: $(BINARY) $(UNIT)
	@echo "Installing DHT Exporter..."
	# Install the binary
	install -m 755 $(BINARY) $(BIN_DIR)/$(BINARY)
	# Install the systemd unit file
	install -m 644 $(UNIT) $(UNIT_DIR)/$(UNIT)
	# Install the configuration file
	install -m 644 $(CONF) $(CONF_DIR)/dht-exporter
	# Create the dedicated service user (no login, no shell)
	useradd -r -s /bin/false $(USER) 2>/dev/null || true
	# Reload systemd to pick up the new unit file
	systemctl daemon-reload
	# Enable the service to start at boot
	systemctl enable $(UNIT)
	@echo "Installation complete. Run 'make start' to start the service."

# Uninstall: stop and disable the service, remove all installed files and the user.
# This target should be run as root (e.g., via sudo make uninstall).
uninstall:
	@echo "Uninstalling DHT Exporter..."
	# Stop the service if it is running
	systemctl stop $(UNIT) 2>/dev/null || true
	# Disable the service (remove from boot targets)
	systemctl disable $(UNIT) 2>/dev/null || true
	# Remove the binary
	rm -f $(BIN_DIR)/$(BINARY)
	# Remove the unit file
	rm -f $(UNIT_DIR)/$(UNIT)
	# Remove the configuration file
	rm -f $(CONF_DIR)/dht-exporter
	# Remove the dedicated user
	userdel $(USER) 2>/dev/null || true
	# Reload systemd to forget the removed unit file
	systemctl daemon-reload
	@echo "Uninstallation complete."

# =========================================================================
# Service management
# =========================================================================

# Start the service
start:
	systemctl start $(UNIT)

# Stop the service
stop:
	systemctl stop $(UNIT)

# Restart the service (picks up configuration changes)
restart:
	systemctl restart $(UNIT)

# Show the service status (running state, recent logs, resource usage)
status:
	systemctl status $(UNIT)

# Follow the service logs in real time (Ctrl-C to stop)
logs:
	journalctl -u $(UNIT) -f

# =========================================================================
# Cleanup
# =========================================================================

# Remove the built binary and Go module files
clean:
	rm -f $(BINARY)
	rm -f go.mod go.sum
	rm -rf vendor
