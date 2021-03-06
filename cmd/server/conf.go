package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/frizinak/homechat/server"
)

type Config struct {
	Directory                 string
	YMDir                     string
	AcoustIDKey               string
	ChatMessagesAppendOnlyDir *string

	ClientPolicy     server.ClientPolicy
	ClientPolicyFile string

	HTTPPublicAddr string
	HTTPBindAddr   string
	TCPBindAddr    string

	BandwidthIntervalSeconds *int
	MaxUploadKBytes          *int64
	MaxChatMessages          int

	WttrCity           string
	HolidayCountryCode string

	HueIP   string
	HuePass string

	resave bool
}

func (c *Config) Help() []string {
	return []string{
		"Directory:                 Data directory",
		"                           Location of all channel logs",
		"",
		"YMDir:                     Libym data directory",
		"                           Location of songs and song database",
		"",
		"AcoustIDKey:               Acoustid application api key",
		"",
		"ChatMessagesAppendOnlyDir: Location of append only chat logs",
		"                           Empty to not store any logs (!)",
		"",
		"ClientPolicy:              Specify the client policy.",
		"                           i.e.: a policy that determines who can connect",
		"                           and with what username.",
		"                           One of:",
		fmt.Sprintf("                             - %-8s: everyone can connect and pick an arbitrary username.", server.PolicyWorld),
		fmt.Sprintf("                             - %-8s: only those in the fingerprint file are allowed.", server.PolicyAllow),
		fmt.Sprintf("                             - %-8s: same as `fingerprint` but force name as well.", server.PolicyFixed),
		"",
		"ClientPolicyFile:          Location of the client policy file",
		"                           Each line should contain exactly one fingerprint and username",
		"                           separated by a space",
		"",
		"HTTPPublicAddr:            The publicly reachable domain or ip:port",
		"                           Used to create download links",
		"",
		"HTTPBindAddr:              ip:port of the http server",
		"                           use 0.0.0.0:1200 to bind to all interfaces",
		"",
		"TCPBindAddr:               ip:port of the tcp server",
		"                           use 0.0.0.0:1201 to bind to all interfaces",
		"",
		"BandwidthIntervalSeconds:  Log bandwidth usage every n seconds",
		"                           0 for no logging",
		"",
		"MaxUploadKBytes:           Maximum file upload size in KiB",
		"",
		"MaxChatMessages:           Maximum amount of chat messages",
		"                           that are kept in the main channel logfile",
		"                           (!) note: the logfile will be truncated to this size",
		"",
		"WttrCity:                  Name of the city to be used as",
		"                           the default for wttr.in bot",
		"",
		"HolidayCountryCode:        2-letter country code to be used as",
		"                           the default for the ",
		"                           https://date.nager.at/api/v2/PublicHolidays bot",
		"",
		"HueIP:                     Philips Hue bridge ip for the hue bot",
		"",
		"HuePass:                   Philips Hue bridge pass for the hue bot",
		"                           You can use https://github.com/amimof/huego",
		"                           or https://github.com/frizinak/hue or ...",
		"                           to find the ip and generate a password",
	}
}

func (c *Config) Decode(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	d := make(map[string]json.RawMessage)
	if err := json.NewDecoder(f).Decode(&d); err != nil {
		return fmt.Errorf("failed to parse server config %s: %w", file, err)
	}

	m := map[string]interface{}{
		"Directory":                 &c.Directory,
		"YMDir":                     &c.YMDir,
		"AcoustIDKey":               &c.AcoustIDKey,
		"ChatMessagesAppendOnlyDir": &c.ChatMessagesAppendOnlyDir,
		"ClientPolicy":              &c.ClientPolicy,
		"ClientPolicyFile":          &c.ClientPolicyFile,
		"HTTPPublicAddr":            &c.HTTPPublicAddr,
		"HTTPBindAddr":              &c.HTTPBindAddr,
		"TCPBindAddr":               &c.TCPBindAddr,
		"BandwidthIntervalSeconds":  &c.BandwidthIntervalSeconds,
		"MaxUploadKBytes":           &c.MaxUploadKBytes,
		"MaxChatMessages":           &c.MaxChatMessages,
		"WttrCity":                  &c.WttrCity,
		"HolidayCountryCode":        &c.HolidayCountryCode,
		"HueIP":                     &c.HueIP,
		"HuePass":                   &c.HuePass,
	}

	for k, field := range m {
		if v, ok := d[k]; ok {
			err = json.Unmarshal(v, field)
			if err != nil {
				return fmt.Errorf("failed to parse client config %s: %w", file, err)
			}
			continue
		}
		c.resave = true
	}

	return nil
}

func (c *Config) Encode(file string) error {
	tmp := file + ".tmp"
	err := func() error {
		f, err := os.Create(tmp)
		if err != nil {
			return err
		}
		defer f.Close()
		enc := json.NewEncoder(f)
		enc.SetIndent("", "    ")
		if err := enc.Encode(c); err != nil {
			return err
		}
		return nil
	}()
	if err != nil {
		return err
	}

	return os.Rename(tmp, file)
}

func (c *Config) Merge(def *Config) bool {
	resave := c.resave

	if c.WttrCity == "" && def.WttrCity != "" {
		resave = true
		c.WttrCity = def.WttrCity
	}
	if c.AcoustIDKey == "" && def.AcoustIDKey != "" {
		resave = true
		c.AcoustIDKey = def.AcoustIDKey
	}
	if c.HolidayCountryCode == "" && def.HolidayCountryCode != "" {
		resave = true
		c.HolidayCountryCode = def.HolidayCountryCode
	}
	if c.YMDir == "" && def.YMDir != "" {
		resave = true
		c.YMDir = def.YMDir
	}
	if c.Directory == "" && def.Directory != "" {
		resave = true
		c.Directory = def.Directory
	}
	if c.HTTPPublicAddr == "" && def.HTTPPublicAddr != "" {
		resave = true
		c.HTTPPublicAddr = def.HTTPPublicAddr
	}
	if c.HTTPBindAddr == "" && def.HTTPBindAddr != "" {
		resave = true
		c.HTTPBindAddr = def.HTTPBindAddr
	}
	if c.BandwidthIntervalSeconds == nil {
		resave = true
		c.BandwidthIntervalSeconds = def.BandwidthIntervalSeconds
	}
	if c.MaxUploadKBytes == nil {
		resave = true
		c.MaxUploadKBytes = def.MaxUploadKBytes
	}
	if c.MaxChatMessages < 0 {
		resave = true
		c.MaxChatMessages = 0
	}
	if c.MaxChatMessages == 0 && def.MaxChatMessages > 0 {
		resave = true
		c.MaxChatMessages = def.MaxChatMessages
	}
	if c.ChatMessagesAppendOnlyDir == nil {
		resave = true
		c.ChatMessagesAppendOnlyDir = def.ChatMessagesAppendOnlyDir
	}
	if c.TCPBindAddr == "" && def.TCPBindAddr != "" {
		resave = true
		c.TCPBindAddr = def.TCPBindAddr
	}
	if c.ClientPolicy == "" && def.ClientPolicy != "" {
		resave = true
		c.ClientPolicy = def.ClientPolicy
	}
	if c.ClientPolicyFile == "" && def.ClientPolicyFile != "" {
		resave = true
		c.ClientPolicyFile = def.ClientPolicyFile
	}
	return resave
}
