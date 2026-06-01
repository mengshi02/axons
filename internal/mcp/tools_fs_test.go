package mcp

import (
	"testing"
)

func TestIsAllowedCommand(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		expected bool
	}{
		// 测试只有命令名的情况
		{"only command name - go", "go", true},
		{"only command name - python", "python", true},
		{"only command name - node", "node", true},
		{"only command name - git", "git", true},

		// 测试命令名带参数的情况
		{"command with args - go test", "go test ./...", true},
		{"command with args - go build", "go build ./internal/app", true},
		{"command with args - python script", "python script.py", true},
		{"command with args - npm install", "npm install", true},
		{"command with args - git status", "git status", true},

		// 测试不在允许列表中的命令
		{"not allowed - rm", "rm", false},
		{"not allowed - rm with args", "rm -rf /", false},
		{"not allowed - sudo", "sudo", false},
		{"not allowed - unknown", "unknown-command", false},

		// 测试空字符串
		{"empty string", "", false},

		// 测试带空格的边界情况
		{"command with multiple spaces", "go  test   ./...", true},
		{"command with leading space", " go test", true},
		{"command with trailing space", "go test ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAllowedCommand(tt.cmd)
			if result != tt.expected {
				t.Errorf("isAllowedCommand(%q) = %v, expected %v", tt.cmd, result, tt.expected)
			}
		})
	}
}