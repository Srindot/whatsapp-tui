package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaults_CmdPrefix(t *testing.T) {
	if Config.General.CmdPrefix != "/" {
		t.Errorf("expected default CmdPrefix '/', got %q", Config.General.CmdPrefix)
	}
}

func TestDefaults_DownloadPath(t *testing.T) {
	if !strings.HasSuffix(Config.General.DownloadPath, "Downloads") {
		t.Errorf("expected DownloadPath to end with 'Downloads', got %q", Config.General.DownloadPath)
	}
}

func TestDefaults_KeyBindings(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"SwitchPanels", Config.Keymap.SwitchPanels, "Tab"},
		{"FocusInput", Config.Keymap.FocusInput, "Ctrl+Space"},
		{"CommandQuit", Config.Keymap.CommandQuit, "Ctrl+q"},
		{"CommandHelp", Config.Keymap.CommandHelp, "Ctrl+?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.got)
			}
		})
	}
}

func TestDefaults_Colors(t *testing.T) {
	if Config.Colors.Background != "black" {
		t.Errorf("expected default Background 'black', got %q", Config.Colors.Background)
	}
	if Config.Colors.Positive != "green" {
		t.Errorf("expected default Positive 'green', got %q", Config.Colors.Positive)
	}
}

func TestDefaults_Ui(t *testing.T) {
	if Config.Ui.ChatSidebarWidth != 38 {
		t.Errorf("expected default ChatSidebarWidth 38, got %d", Config.Ui.ChatSidebarWidth)
	}
}

func TestGetHomeDir(t *testing.T) {
	home := GetHomeDir()
	if home == "" {
		t.Error("GetHomeDir returned empty string")
	}
	if !strings.HasSuffix(home, string(os.PathSeparator)) {
		t.Errorf("GetHomeDir should end with path separator, got %q", home)
	}
}

func TestGetSessionFilePath(t *testing.T) {
	path := GetSessionFilePath()
	if path == "" {
		t.Error("GetSessionFilePath returned empty string")
	}
}

func TestExpandPath(t *testing.T) {
	home := GetHomeDir()
	t.Setenv("WTUI_TEST_DIR", "/tmp/x")
	tests := map[string]string{
		"~/Downloads":           filepath.Join(home, "Downloads"),
		"~":                     filepath.Clean(home),
		"$WTUI_TEST_DIR/pics/":  "/tmp/x/pics",
		"  /srv/media//files  ": "/srv/media/files",
	}
	for in, want := range tests {
		if got := ExpandPath(in); got != want {
			t.Errorf("ExpandPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetDownloadPathPersists(t *testing.T) {
	old := configFilePath
	defer func() { configFilePath = old }()
	configFilePath = filepath.Join(t.TempDir(), "config.ini")
	if err := os.WriteFile(configFilePath, []byte("[general]\ndownload_path = /old\ncmd_prefix = /\n\n[ui]\ntheme = rose-pine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetDownloadPath("/new/place"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(configFilePath)
	s := string(b)
	if !strings.Contains(s, "/new/place") || strings.Contains(s, "/old") || !strings.Contains(s, "rose-pine") {
		t.Fatalf("config after save:\n%s", s)
	}
	if Config.General.DownloadPath != "/new/place" {
		t.Fatalf("in-memory path = %q", Config.General.DownloadPath)
	}
}
