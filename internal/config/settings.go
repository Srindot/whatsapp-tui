package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
	"gopkg.in/ini.v1"
)

var configFilePath string
var cfg *ini.File

type IniFile struct {
	*General
	*Keymap
	*Ui
	*Colors
}

type General struct {
	DownloadPath        string
	PreviewPath         string
	CmdPrefix           string
	ShowCommand         string
	EnableNotifications bool
	UseTerminalBell     bool
	NotificationTimeout int64
	BacklogMsgQuantity  int
}

type Keymap struct {
	SwitchPanels    string
	FocusMessages   string
	FocusInput      string
	FocusChats      string
	Copyuser        string
	Pasteuser       string
	CommandBacklog  string
	CommandRead     string
	CommandConnect  string
	CommandQuit     string
	CommandHelp     string
	MessageDownload string
	MessageOpen     string
	MessageShow     string
	MessageUrl      string
	MessageInfo     string
	MessageRevoke   string
}

type Ui struct {
	ChatSidebarWidth int
	QrCompact        bool
	Theme            string // rose-pine, rose-pine-moon or rose-pine-dawn
	PaintBackground  bool   // fill the screen with the theme background
	Images           string // auto, kitty, blocks or off
	Avatars          bool   // show profile pictures (kitty only)
	Mouse            bool   // click a chat to open it, wheel to scroll
	// HighlightOpacity: how solid highlights, bubbles and the status bar are
	// over a see-through terminal (0..1; 1 = solid). kitty only.
	HighlightOpacity float64
}

type Colors struct {
	Background      string
	Text            string
	ForwardedText   string
	ListHeader      string
	ListContact     string
	ListGroup       string
	ChatContact     string
	ChatMe          string
	Borders         string
	InputBackground string
	InputText       string
	UnreadCount     string
	Positive        string
	Negative        string
}

var Config = IniFile{
	&General{
		DownloadPath:        GetHomeDir() + "Downloads",
		PreviewPath:         GetHomeDir() + "Downloads",
		CmdPrefix:           "/",
		ShowCommand:         "jp2a --color",
		EnableNotifications: false,
		UseTerminalBell:     false,
		NotificationTimeout: 60,
		BacklogMsgQuantity:  10,
	},
	&Keymap{
		SwitchPanels:    "Tab",
		FocusMessages:   "Ctrl+w",
		FocusInput:      "Ctrl+Space",
		FocusChats:      "Ctrl+e",
		CommandBacklog:  "Ctrl+b",
		CommandRead:     "Ctrl+n",
		Copyuser:        "Ctrl+c",
		Pasteuser:       "Ctrl+v",
		CommandConnect:  "Ctrl+r",
		CommandQuit:     "Ctrl+q",
		CommandHelp:     "Ctrl+?",
		MessageDownload: "d",
		MessageInfo:     "i",
		MessageOpen:     "o",
		MessageUrl:      "u",
		MessageRevoke:   "r",
		MessageShow:     "s",
	},
	&Ui{
		ChatSidebarWidth: 38,
		QrCompact:        false,
		Theme:            "rose-pine",
		PaintBackground:  false,
		Images:           "auto",
		Avatars:          true,
		Mouse:            true,
		HighlightOpacity: 0.8,
	},
	&Colors{
		Background:      "black",
		Text:            "white",
		ForwardedText:   "purple",
		ListHeader:      "yellow",
		ListContact:     "green",
		ListGroup:       "blue",
		ChatContact:     "green",
		ChatMe:          "blue",
		Borders:         "white",
		InputBackground: "blue",
		InputText:       "white",
		UnreadCount:     "yellow",
		Positive:        "green",
		Negative:        "red",
	},
}

func InitConfig() error {
	var err error
	if configFilePath, err = xdg.ConfigFile("whatsapp-tui/config.ini"); err == nil {
		// add any new values
		var cfg *ini.File
		if cfg, err = ini.Load(configFilePath); err == nil {
			cfg.NameMapper = ini.TitleUnderscore
			cfg.ValueMapper = os.ExpandEnv
			if section, err := cfg.GetSection("general"); err == nil {
				section.MapTo(&Config.General)
			}
			if section, err := cfg.GetSection("keymap"); err == nil {
				section.MapTo(&Config.Keymap)
			}
			if section, err := cfg.GetSection("ui"); err == nil {
				section.MapTo(&Config.Ui)
			}
			if section, err := cfg.GetSection("colors"); err == nil {
				section.MapTo(&Config.Colors)
			}
		} else {
			cfg = ini.Empty()
			cfg.NameMapper = ini.TitleUnderscore
			cfg.ValueMapper = os.ExpandEnv
			if err = ini.ReflectFromWithMapper(cfg, &Config, ini.TitleUnderscore); err == nil {
				err = cfg.SaveTo(configFilePath)
			}
		}
	}
	return err
}

func GetConfigFilePath() string {
	return configFilePath
}

// GetCacheDir returns the directory for downloaded media and avatars.
func GetCacheDir() string {
	return filepath.Join(xdg.CacheHome, "whatsapp-tui")
}

func GetSessionFilePath() string {
	if sessionFilePath, err := xdg.ConfigFile("whatsapp-tui/session"); err == nil {
		return sessionFilePath
	}
	return GetHomeDir() + ".whatsapp-tui.session"
}

// gets the OS home dir with a path separator at the end
func GetHomeDir() string {
	usr, err := user.Current()
	if err == nil {
		return usr.HomeDir + string(os.PathSeparator)
	}
	// Fallback to environment variable
	home := os.Getenv("HOME")
	if home != "" {
		return home + string(os.PathSeparator)
	}
	return "." + string(os.PathSeparator)
}

// ExpandPath expands a leading "~" and environment variables.
func ExpandPath(p string) string {
	p = os.ExpandEnv(strings.TrimSpace(p))
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = filepath.Join(GetHomeDir(), strings.TrimPrefix(p, "~"))
	}
	return filepath.Clean(p)
}

// SetDownloadPath changes where downloads are saved and writes it to the
// config file, keeping the rest of the file as it is.
func SetDownloadPath(p string) error {
	p = ExpandPath(p)
	if configFilePath == "" {
		Config.General.DownloadPath = p
		return nil
	}
	f, err := ini.Load(configFilePath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	f.Section("general").Key("download_path").SetValue(p)
	if err := f.SaveTo(configFilePath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	Config.General.DownloadPath = p
	return nil
}
