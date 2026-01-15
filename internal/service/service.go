// Package service provides system service configuration for the gt daemon.
// It supports launchd (macOS) and systemd (Linux) for external supervision.
package service

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

//go:embed gastown-daemon.plist
var launchdPlist []byte

//go:embed gastown-daemon.service
var systemdService []byte

// ServiceType represents the type of service manager.
type ServiceType string

const (
	Launchd ServiceType = "launchd" // macOS
	Systemd ServiceType = "systemd" // Linux
)

// DetectServiceType detects the appropriate service type for the current platform.
func DetectServiceType() (ServiceType, error) {
	switch runtime.GOOS {
	case "darwin":
		return Launchd, nil
	case "linux":
		return Systemd, nil
	default:
		return "", fmt.Errorf("unsupported platform: %s (no service manager available)", runtime.GOOS)
	}
}

// Config holds configuration for generating service files.
type Config struct {
	GTPath    string // Full path to gt executable
	TownRoot  string // Full path to town root (workspace)
	TownName  string // Name of the town
}

// Install installs the service file for the current platform.
func Install(cfg Config) error {
	serviceType, err := DetectServiceType()
	if err != nil {
		return err
	}

	switch serviceType {
	case Launchd:
		return installLaunchd(cfg)
	case Systemd:
		return installSystemd(cfg)
	default:
		return fmt.Errorf("unsupported service type: %s", serviceType)
	}
}

// installLaunchd creates and installs the launchd plist file.
func installLaunchd(cfg Config) error {
	// Expand user home in town root
	townRoot := cfg.TownRoot
	if len(townRoot) > 0 && townRoot[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}
		townRoot = filepath.Join(home, townRoot[1:])
	}

	// Create LaunchAgents directory if it doesn't exist
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	launchAgentsDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(launchAgentsDir, 0755); err != nil {
		return fmt.Errorf("creating LaunchAgents directory: %w", err)
	}

	// Render and write plist file
	plistPath := filepath.Join(launchAgentsDir, "com.gastown.daemon.plist")
	content := fmt.Sprintf(string(launchdPlist), cfg.GTPath, townRoot, townRoot, townRoot)
	if err := os.WriteFile(plistPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing plist file: %w", err)
	}

	return nil
}

// installSystemd creates and installs the systemd unit file.
func installSystemd(cfg Config) error {
	// For systemd, we install to user unit directory
	// Use systemd environment variable if set, otherwise use default
	unitDir := os.Getenv("SYSTEMD_USER_UNIT_DIR")
	if unitDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}
		// Try to get XDG_CONFIG_HOME, default to ~/.config
		configDir := os.Getenv("XDG_CONFIG_HOME")
		if configDir == "" {
			configDir = filepath.Join(home, ".config")
		}
		unitDir = filepath.Join(configDir, "systemd", "user")
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return fmt.Errorf("creating systemd user unit directory: %w", err)
	}

	// Render and write service file
	servicePath := filepath.Join(unitDir, "gastown-daemon.service")
	content := fmt.Sprintf(string(systemdService), cfg.TownName, cfg.GTPath, cfg.TownRoot, cfg.TownRoot, cfg.TownRoot)
	if err := os.WriteFile(servicePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing service file: %w", err)
	}

	return nil
}

// Uninstall removes the service file.
func Uninstall() error {
	serviceType, err := DetectServiceType()
	if err != nil {
		return err
	}

	switch serviceType {
	case Launchd:
		return uninstallLaunchd()
	case Systemd:
		return uninstallSystemd()
	default:
		return fmt.Errorf("unsupported service type: %s", serviceType)
	}
}

// uninstallLaunchd removes the launchd plist file.
func uninstallLaunchd() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.gastown.daemon.plist")
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing plist file: %w", err)
	}
	return nil
}

// uninstallSystemd removes the systemd unit file.
func uninstallSystemd() error {
	unitDir := os.Getenv("SYSTEMD_USER_UNIT_DIR")
	if unitDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}
		configDir := os.Getenv("XDG_CONFIG_HOME")
		if configDir == "" {
			configDir = filepath.Join(home, ".config")
		}
		unitDir = filepath.Join(configDir, "systemd", "user")
	}
	servicePath := filepath.Join(unitDir, "gastown-daemon.service")
	if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing service file: %w", err)
	}
	return nil
}

// StartCommand returns the command to start the service.
func StartCommand() (string, error) {
	serviceType, err := DetectServiceType()
	if err != nil {
		return "", err
	}

	switch serviceType {
	case Launchd:
		return "launchctl load ~/Library/LaunchAgents/com.gastown.daemon.plist", nil
	case Systemd:
		return "systemctl --user daemon-reload && systemctl --user enable gastown-daemon.service && systemctl --user start gastown-daemon.service", nil
	default:
		return "", fmt.Errorf("unsupported service type: %s", serviceType)
	}
}

// StopCommand returns the command to stop the service.
func StopCommand() (string, error) {
	serviceType, err := DetectServiceType()
	if err != nil {
		return "", err
	}

	switch serviceType {
	case Launchd:
		return "launchctl unload ~/Library/LaunchAgents/com.gastown.daemon.plist", nil
	case Systemd:
		return "systemctl --user stop gastown-daemon.service", nil
	default:
		return "", fmt.Errorf("unsupported service type: %s", serviceType)
	}
}

// StatusCommand returns the command to check service status.
func StatusCommand() (string, error) {
	serviceType, err := DetectServiceType()
	if err != nil {
		return "", err
	}

	switch serviceType {
	case Launchd:
		return "launchctl list | grep com.gastown.daemon", nil
	case Systemd:
		return "systemctl --user status gastown-daemon.service", nil
	default:
		return "", fmt.Errorf("unsupported service type: %s", serviceType)
	}
}
