package config

import (
	"os"
	"reflect"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected *Config
	}{
		{
			name:    "default values",
			envVars: map[string]string{},
			expected: &Config{
				HTTPPort:         8080,
				GRPCPort:         50051,
				HealthCheckPort:  8082,
				LogLevel:         "info",
				ForwardTo:        []string{},
				ForwardTimeout:   30 * time.Second,
				MaxEventSize:     1024 * 1024,
				BufferSize:       1000,
				MaxConnections:   100,
				Version:          "dev",
				Forwarding:       nil,
			},
		},
		{
			name: "custom environment variables",
			envVars: map[string]string{
				"HTTP_PORT":      "9090",
				"GRPC_PORT":      "9091",
				"LOG_LEVEL":      "debug",
				"BUFFER_SIZE":    "2000",
				"MAX_EVENT_SIZE": "2097152",
			},
			expected: &Config{
				HTTPPort:         9090,
				GRPCPort:         9091,
				HealthCheckPort:  8082,
				LogLevel:         "debug",
				ForwardTo:        []string{},
				ForwardTimeout:   30 * time.Second,
				MaxEventSize:     2097152,
				BufferSize:       2000,
				MaxConnections:   100,
				Version:          "dev",
				Forwarding:       nil,
			},
		},
		{
			name: "forwarding enabled",
			envVars: map[string]string{
				"FORWARDING_ENABLED": "true",
				"FORWARDING_WORKERS": "10",
			},
			expected: &Config{
				HTTPPort:         8080,
				GRPCPort:         50051,
				HealthCheckPort:  8082,
				LogLevel:         "info",
				ForwardTo:        []string{},
				ForwardTimeout:   30 * time.Second,
				MaxEventSize:     1024 * 1024,
				BufferSize:       1000,
				MaxConnections:   100,
				Version:          "dev",
				Forwarding: &ForwardingConfig{
					Enabled:             true,
					DefaultRetryAttempts: 3,
					DefaultTimeout:      30 * time.Second,
					BufferSize:          1000,
					Workers:             10,
					Targets:             []*TargetConfiguration{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			// Clear environment variables after test
			defer func() {
				for key := range tt.envVars {
					os.Unsetenv(key)
				}
			}()

			config := Load()

			if !reflect.DeepEqual(config, tt.expected) {
				t.Errorf("Load() = %+v, want %+v", config, tt.expected)
			}
		})
	}
}

func TestLoadFromFile(t *testing.T) {
	// Create temporary config file
	configContent := `{
		"HTTPPort": 9000,
		"GRPCPort": 9001,
		"LogLevel": "warn",
		"forwarding": {
			"enabled": true,
			"workers": 5,
			"targets": [
				{
					"id": "test-target",
					"name": "Test Target",
					"type": "http",
					"endpoint": "http://example.com"
				}
			]
		}
	}`

	tmpFile, err := os.CreateTemp("", "config-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	config, err := LoadFromFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	expected := &Config{
		HTTPPort:        9000,
		GRPCPort:        9001,
		HealthCheckPort: 8082, // Default from env override
		LogLevel:        "warn",
		Forwarding: &ForwardingConfig{
			Enabled: true,
			Workers: 5,
			Targets: []*TargetConfiguration{
				{
					ID:       "test-target",
					Name:     "Test Target",
					Type:     "http",
					Endpoint: "http://example.com",
				},
			},
		},
	}

	if config.HTTPPort != expected.HTTPPort {
		t.Errorf("HTTPPort = %d, want %d", config.HTTPPort, expected.HTTPPort)
	}
	if config.GRPCPort != expected.GRPCPort {
		t.Errorf("GRPCPort = %d, want %d", config.GRPCPort, expected.GRPCPort)
	}
	if config.LogLevel != expected.LogLevel {
		t.Errorf("LogLevel = %s, want %s", config.LogLevel, expected.LogLevel)
	}
	if config.Forwarding == nil {
		t.Error("Forwarding config is nil")
	} else {
		if config.Forwarding.Enabled != expected.Forwarding.Enabled {
			t.Errorf("Forwarding.Enabled = %v, want %v", config.Forwarding.Enabled, expected.Forwarding.Enabled)
		}
		if len(config.Forwarding.Targets) != 1 {
			t.Errorf("Number of targets = %d, want 1", len(config.Forwarding.Targets))
		}
	}
}

func TestLoadFromFile_InvalidJSON(t *testing.T) {
	// Create temporary config file with invalid JSON
	configContent := `{
		"HTTPPort": 9000,
		"GRPCPort": 9001,
		"LogLevel": "warn",
		// This comment makes it invalid JSON
	}`

	tmpFile, err := os.CreateTemp("", "config-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	_, err = LoadFromFile(tmpFile.Name())
	if err == nil {
		t.Error("LoadFromFile() should return error for invalid JSON")
	}
}

func TestGetForwardingTargetByID(t *testing.T) {
	config := &Config{
		Forwarding: &ForwardingConfig{
			Targets: []*TargetConfiguration{
				{ID: "target1", Name: "Target 1"},
				{ID: "target2", Name: "Target 2"},
			},
		},
	}

	tests := []struct {
		name       string
		id         string
		expected   *TargetConfiguration
		shouldFind bool
	}{
		{
			name:       "existing target",
			id:         "target1",
			expected:   &TargetConfiguration{ID: "target1", Name: "Target 1"},
			shouldFind: true,
		},
		{
			name:       "non-existing target",
			id:         "target3",
			expected:   nil,
			shouldFind: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := config.GetForwardingTargetByID(tt.id)
			
			if tt.shouldFind {
				if result == nil {
					t.Error("Expected to find target, but got nil")
				} else if result.ID != tt.expected.ID {
					t.Errorf("Target ID = %s, want %s", result.ID, tt.expected.ID)
				}
			} else {
				if result != nil {
					t.Errorf("Expected nil, but got target: %+v", result)
				}
			}
		})
	}
}

func TestAddForwardingTarget(t *testing.T) {
	config := &Config{}

	target := &TargetConfiguration{
		ID:   "new-target",
		Name: "New Target",
		Type: "http",
	}

	// Test adding to nil forwarding config
	err := config.AddForwardingTarget(target)
	if err != nil {
		t.Errorf("AddForwardingTarget() error = %v", err)
	}

	if config.Forwarding == nil {
		t.Error("Forwarding config should be initialized")
	}

	if len(config.Forwarding.Targets) != 1 {
		t.Errorf("Number of targets = %d, want 1", len(config.Forwarding.Targets))
	}

	// Test adding duplicate target
	err = config.AddForwardingTarget(target)
	if err == nil {
		t.Error("AddForwardingTarget() should return error for duplicate ID")
	}
}

func TestRemoveForwardingTarget(t *testing.T) {
	config := &Config{
		Forwarding: &ForwardingConfig{
			Targets: []*TargetConfiguration{
				{ID: "target1", Name: "Target 1"},
				{ID: "target2", Name: "Target 2"},
			},
		},
	}

	// Test removing existing target
	err := config.RemoveForwardingTarget("target1")
	if err != nil {
		t.Errorf("RemoveForwardingTarget() error = %v", err)
	}

	if len(config.Forwarding.Targets) != 1 {
		t.Errorf("Number of targets = %d, want 1", len(config.Forwarding.Targets))
	}

	// Test removing non-existing target
	err = config.RemoveForwardingTarget("target3")
	if err == nil {
		t.Error("RemoveForwardingTarget() should return error for non-existing target")
	}
}

func TestGetEnvAsBool(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		value        string
		defaultValue bool
		expected     bool
	}{
		{"true string", "TEST_BOOL", "true", false, true},
		{"false string", "TEST_BOOL", "false", true, false},
		{"1 string", "TEST_BOOL", "1", false, true},
		{"0 string", "TEST_BOOL", "0", true, false},
		{"yes string", "TEST_BOOL", "yes", false, true},
		{"no string", "TEST_BOOL", "no", true, false},
		{"on string", "TEST_BOOL", "on", false, true},
		{"off string", "TEST_BOOL", "off", true, false},
		{"invalid string", "TEST_BOOL", "invalid", false, false},
		{"empty value", "TEST_BOOL", "", true, true},
		{"unset variable", "UNSET_VAR", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != "" {
				os.Setenv(tt.key, tt.value)
				defer os.Unsetenv(tt.key)
			}

			result := getEnvAsBool(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnvAsBool(%s, %v) = %v, want %v", tt.key, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

func TestSaveToFile(t *testing.T) {
	config := &Config{
		HTTPPort: 9000,
		GRPCPort: 9001,
		LogLevel: "debug",
		Forwarding: &ForwardingConfig{
			Enabled: true,
			Workers: 5,
		},
	}

	tmpFile, err := os.CreateTemp("", "config-save-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	err = config.SaveToFile(tmpFile.Name())
	if err != nil {
		t.Errorf("SaveToFile() error = %v", err)
	}

	// Verify file was written correctly
	loadedConfig, err := LoadFromFile(tmpFile.Name())
	if err != nil {
		t.Errorf("Failed to load saved file: %v", err)
	}

	if loadedConfig.HTTPPort != config.HTTPPort {
		t.Errorf("Saved HTTPPort = %d, want %d", loadedConfig.HTTPPort, config.HTTPPort)
	}
}