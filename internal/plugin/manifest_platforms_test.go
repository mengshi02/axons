package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolvePlatforms_NoPlatforms(t *testing.T) {
	// When Platforms is nil, resolvePlatforms should be a no-op
	m := &PluginManifest{
		ID:      "com.test.plugin",
		Name:    "Test",
		Version: "1.0.0",
		Backend: &BackendDef{
			Command:     []string{".venv/bin/python", "server.py"},
			HealthCheck: "/health",
		},
	}
	original := m.Backend.Command

	resolvePlatforms(m)

	if m.Backend.Platforms != nil {
		t.Error("Platforms should remain nil")
	}
	// Command should not change
	if len(m.Backend.Command) != len(original) || m.Backend.Command[0] != original[0] {
		t.Errorf("Command changed unexpectedly: got %v, want %v", m.Backend.Command, original)
	}
}

func TestResolvePlatforms_NoMatchingOS(t *testing.T) {
	// When Platforms exists but no override for current OS, should clear Platforms
	otherOS := "nonexistent-os"
	m := &PluginManifest{
		ID:      "com.test.plugin",
		Name:    "Test",
		Version: "1.0.0",
		Backend: &BackendDef{
			Command:     []string{".venv/bin/python", "server.py"},
			HealthCheck: "/health",
			Platforms: map[string]*PlatformOverride{
				otherOS: {
					Command: []string{".venv\\Scripts\\python.exe", "server.py"},
				},
			},
		},
	}

	resolvePlatforms(m)

	if m.Backend.Platforms != nil {
		t.Error("Platforms should be cleared after resolve")
	}
	// Command should NOT change since current OS doesn't match
	if m.Backend.Command[0] != ".venv/bin/python" {
		t.Errorf("Command should not change: got %v", m.Backend.Command)
	}
}

func TestResolvePlatforms_MatchingOS(t *testing.T) {
	// When Platforms has override for current OS, should apply it
	currentOS := runtime.GOOS

	m := &PluginManifest{
		ID:      "com.test.plugin",
		Name:    "Test",
		Version: "1.0.0",
		Backend: &BackendDef{
			Command:     []string{".venv/bin/python", "server.py"},
			Port:        0,
			HealthCheck: "/health",
			ReadyTimeout: "10s",
			Env:         map[string]string{"KEY1": "value1"},
			Install: &InstallDef{
				Command: []string{"bash", "install.sh"},
				Timeout: "120s",
			},
			Platforms: map[string]*PlatformOverride{
				currentOS: {
					Command: []string{".venv/Scripts/python.exe", "server.py"},
					Env:     map[string]string{"KEY2": "value2"},
					Install: &InstallDef{
						Command: []string{"cmd", "/c", "install.bat"},
						Timeout: "300s",
					},
				},
			},
		},
	}

	resolvePlatforms(m)

	// Platforms should be cleared
	if m.Backend.Platforms != nil {
		t.Error("Platforms should be cleared after resolve")
	}

	// Command should be overridden
	if m.Backend.Command[0] != ".venv/Scripts/python.exe" {
		t.Errorf("Command not overridden: got %v", m.Backend.Command)
	}

	// Env should be merged (KEY1 from base + KEY2 from override)
	if m.Backend.Env["KEY1"] != "value1" {
		t.Error("Base env KEY1 should be preserved")
	}
	if m.Backend.Env["KEY2"] != "value2" {
		t.Error("Override env KEY2 should be added")
	}

	// Install should be overridden entirely
	if m.Backend.Install.Command[0] != "cmd" {
		t.Errorf("Install command not overridden: got %v", m.Backend.Install.Command)
	}
	if m.Backend.Install.Timeout != "300s" {
		t.Errorf("Install timeout not overridden: got %v", m.Backend.Install.Timeout)
	}

	// Non-overridable fields should NOT change
	if m.Backend.Port != 0 {
		t.Error("Port should not be overridden")
	}
	if m.Backend.HealthCheck != "/health" {
		t.Error("HealthCheck should not be overridden")
	}
	if m.Backend.ReadyTimeout != "10s" {
		t.Error("ReadyTimeout should not be overridden")
	}
}

func TestResolvePlatforms_PartialOverride(t *testing.T) {
	// When override only specifies Command, other fields should keep base values
	currentOS := runtime.GOOS

	m := &PluginManifest{
		ID:      "com.test.plugin",
		Name:    "Test",
		Version: "1.0.0",
		Backend: &BackendDef{
			Command:     []string{".venv/bin/python", "server.py"},
			HealthCheck: "/health",
			Install: &InstallDef{
				Command: []string{"bash", "install.sh"},
				Timeout: "120s",
			},
			Platforms: map[string]*PlatformOverride{
				currentOS: {
					Command: []string{".venv/Scripts/python.exe", "server.py"},
				},
			},
		},
	}

	resolvePlatforms(m)

	// Command overridden
	if m.Backend.Command[0] != ".venv/Scripts/python.exe" {
		t.Errorf("Command not overridden: got %v", m.Backend.Command)
	}
	// Install preserved from base
	if m.Backend.Install.Command[0] != "bash" {
		t.Errorf("Install should be preserved: got %v", m.Backend.Install.Command)
	}
	if m.Backend.Install.Timeout != "120s" {
		t.Errorf("Install timeout should be preserved: got %v", m.Backend.Install.Timeout)
	}
}

func TestResolvePlatforms_NilBackend(t *testing.T) {
	// Pure frontend plugin — no backend, should not panic
	m := &PluginManifest{
		ID:       "com.test.plugin",
		Name:     "Test",
		Version:  "1.0.0",
		Frontend: &FrontendDef{Entry: "ui/index.js"},
	}

	resolvePlatforms(m) // should not panic
	_ = m
}

func TestValidateManifest_PlatformsInvalidKey(t *testing.T) {
	m := &PluginManifest{
		ID:      "com.test.plugin",
		Name:    "Test",
		Version: "1.0.0",
		Backend: &BackendDef{
			Command:     []string{"python", "server.py"},
			HealthCheck: "/health",
			Platforms: map[string]*PlatformOverride{
				"android": { // invalid key
					Command: []string{"python3", "server.py"},
				},
			},
		},
	}

	err := ValidateManifest(m)
	if err == nil {
		t.Fatal("Expected error for invalid platform key")
	}
	if !contains(err.Error(), "android") {
		t.Errorf("Error should mention 'android': got %v", err)
	}
}

func TestValidateManifest_PlatformsEmptyOverride(t *testing.T) {
	m := &PluginManifest{
		ID:      "com.test.plugin",
		Name:    "Test",
		Version: "1.0.0",
		Backend: &BackendDef{
			Command:     []string{"python", "server.py"},
			HealthCheck: "/health",
			Platforms: map[string]*PlatformOverride{
				"windows": {}, // empty override
			},
		},
	}

	err := ValidateManifest(m)
	if err == nil {
		t.Fatal("Expected error for empty platform override")
	}
}

func TestValidateManifest_PlatformsInstallNoCommand(t *testing.T) {
	m := &PluginManifest{
		ID:      "com.test.plugin",
		Name:    "Test",
		Version: "1.0.0",
		Backend: &BackendDef{
			Command:     []string{"python", "server.py"},
			HealthCheck: "/health",
			Platforms: map[string]*PlatformOverride{
				"windows": {
					Install: &InstallDef{Timeout: "300s"}, // no command
				},
			},
		},
	}

	err := ValidateManifest(m)
	if err == nil {
		t.Fatal("Expected error for platform install without command")
	}
	if !contains(err.Error(), "windows") {
		t.Errorf("Error should mention 'windows': got %v", err)
	}
}

func TestValidateManifest_PlatformsUninstallNoCommand(t *testing.T) {
	m := &PluginManifest{
		ID:      "com.test.plugin",
		Name:    "Test",
		Version: "1.0.0",
		Backend: &BackendDef{
			Command:     []string{"python", "server.py"},
			HealthCheck: "/health",
			Platforms: map[string]*PlatformOverride{
				"windows": {
					Uninstall: &UninstallDef{}, // no command
				},
			},
		},
	}

	err := ValidateManifest(m)
	if err == nil {
		t.Fatal("Expected error for platform uninstall without command")
	}
}

func TestValidateManifest_PlatformsValid(t *testing.T) {
	m := &PluginManifest{
		ID:      "com.test.plugin",
		Name:    "Test",
		Version: "1.0.0",
		Backend: &BackendDef{
			Command:     []string{"python", "server.py"},
			HealthCheck: "/health",
			Platforms: map[string]*PlatformOverride{
				"windows": {
					Command: []string{".venv\\Scripts\\python.exe", "server.py"},
					Install: &InstallDef{
						Command: []string{"cmd", "/c", "install.bat"},
						Timeout: "300s",
					},
				},
			},
		},
	}

	err := ValidateManifest(m)
	if err != nil {
		t.Errorf("Valid platforms should not error: got %v", err)
	}
}

func TestLoadManifest_WithPlatforms(t *testing.T) {
	// Create a temp plugin directory with manifest.json containing platforms
	dir := t.TempDir()

	manifestJSON := `{
		"id": "com.test.cross-platform",
		"name": "Cross Platform Test",
		"version": "1.0.0",
		"backend": {
			"command": [".venv/bin/python", "server.py"],
			"port": 0,
			"healthCheck": "/health",
			"install": {
				"command": ["bash", "install.sh"],
				"timeout": "120s"
			},
			"platforms": {
				"windows": {
					"command": [".venv\\Scripts\\python.exe", "server.py"],
					"install": {
						"command": ["cmd", "/c", "install.bat"],
						"timeout": "300s"
					}
				}
			}
		}
	}`

	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifestJSON), 0644); err != nil {
		t.Fatal(err)
	}

	manifest, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}

	// Platforms should be resolved and cleared
	if manifest.Backend.Platforms != nil {
		t.Error("Platforms should be nil after LoadManifest (resolved)")
	}

	// On darwin/linux: command should remain the default
	// On windows: command should be overridden
	currentOS := runtime.GOOS
	if currentOS == "windows" {
		if manifest.Backend.Command[0] != ".venv\\Scripts\\python.exe" {
			t.Errorf("Windows: command should be overridden, got %v", manifest.Backend.Command)
		}
		if manifest.Backend.Install.Command[0] != "cmd" {
			t.Errorf("Windows: install command should be overridden, got %v", manifest.Backend.Install.Command)
		}
	} else {
		if manifest.Backend.Command[0] != ".venv/bin/python" {
			t.Errorf("Unix: command should remain default, got %v", manifest.Backend.Command)
		}
		if manifest.Backend.Install.Command[0] != "bash" {
			t.Errorf("Unix: install command should remain default, got %v", manifest.Backend.Install.Command)
		}
	}
}

func TestLoadManifest_WithoutPlatforms(t *testing.T) {
	// Ensure existing manifests without platforms still work
	dir := t.TempDir()

	manifestJSON := `{
		"id": "com.test.basic",
		"name": "Basic Test",
		"version": "1.0.0",
		"backend": {
			"command": ["python", "server.py"],
			"port": 0,
			"healthCheck": "/health"
		}
	}`

	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifestJSON), 0644); err != nil {
		t.Fatal(err)
	}

	manifest, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}

	if manifest.Backend.Command[0] != "python" {
		t.Errorf("Command should be unchanged, got %v", manifest.Backend.Command)
	}
	if manifest.Backend.Platforms != nil {
		t.Error("Platforms should be nil for manifest without platforms")
	}
}

// helper
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}