package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type NotifyWhen string

const (
	NotifyDefault NotifyWhen = "default"
	NotifyAlways  NotifyWhen = "always"
)

type Config struct {
	NotifyCommand     *string
	NotifyWhen        NotifyWhen
	ServerAddress     string
	ServerTCPAddress  string
	ServerFingerprint string
	Username          string
	MaxMessages       int
	MusicDownloads    string
	MusicSocketFile   string
	OpenURLCommand    string
	Zug               bool

	resave bool
}

func (c *Config) Help() []string {
	return []string{
		"NotifyCommand:    program to run to show a notification",
		"                  %u will be replaced with the username of the sender",
		"                  %m will be replaced with the message",
		"                  example: notify-send 'Homechat' '%u: %m'",
		"",
		"NotifyWhen:       When to trigger the above command",
		"                  one off 'default' or 'always'",
		"                  default: direct messages or chat messages that start with '!'",
		"                  always:  well... always",
		"",
		"ServerAddress:    ip:port of the http server",
		"",
		"ServerTCPAddress: ip:port of the tcp server",
		"",
		"Username:         your desired username",
		"",
		"MaxMessages:      maximum amount of messages shown",
		"",
		"MusicDownloads:   path where music download will be stored",
		"",
		"MusicSocketFile:  when not compiled with libmpv use this path for the socket file",
		"                  can be useful when the libym downloads path is on a filesystem",
		"                  that does not support fifos",
		"",
		"OpenURLCommand:   command to run to open urls (%u will be replaced with the url)",
		"                  leave empty to use system defaults",
		"Zug:              true:  enable inline images using zug",
		"                  false: disable inline images",
		"                  only works if you run X11",
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
		return fmt.Errorf("failed to parse client config %s: %w", file, err)
	}

	m := map[string]interface{}{
		"NotifyCommand":     &c.NotifyCommand,
		"NotifyWhen":        &c.NotifyWhen,
		"ServerAddress":     &c.ServerAddress,
		"ServerTCPAddress":  &c.ServerTCPAddress,
		"ServerFingerprint": &c.ServerFingerprint,
		"Username":          &c.Username,
		"MaxMessages":       &c.MaxMessages,
		"MusicDownloads":    &c.MusicDownloads,
		"MusicSocketFile":   &c.MusicSocketFile,
		"OpenURLCommand":    &c.OpenURLCommand,
		"Zug":               &c.Zug,
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

	if c.NotifyCommand == nil {
		resave = true
		c.NotifyCommand = def.NotifyCommand
	}
	if c.NotifyWhen == "" && def.NotifyWhen != "" {
		resave = true
		c.NotifyWhen = def.NotifyWhen
	}
	if c.ServerAddress == "" && def.ServerAddress != "" {
		resave = true
		c.ServerAddress = def.ServerAddress
	}
	if c.ServerTCPAddress == "" && def.ServerTCPAddress != "" {
		resave = true
		c.ServerTCPAddress = def.ServerTCPAddress
	}
	if c.MaxMessages < 0 {
		resave = true
		c.MaxMessages = 0
	}
	if c.MaxMessages == 0 && def.MaxMessages > 0 {
		resave = true
		c.MaxMessages = def.MaxMessages
	}
	if c.MusicDownloads == "" && def.MusicDownloads != "" {
		resave = true
		c.MusicDownloads = def.MusicDownloads
	}
	if c.MusicSocketFile == "" && def.MusicSocketFile != "" {
		resave = true
		c.MusicSocketFile = def.MusicSocketFile
	}

	return resave
}

type Keymap map[Action]string

func (c Keymap) Decode(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&c); err != nil {
		return fmt.Errorf("Failed to parse client config %s: %w", file, err)
	}
	return nil
}

func (c Keymap) Encode(file string) error {
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

func (c Keymap) Merge(def Keymap) bool {
	resave := false
	for i := range def {
		if _, ok := c[i]; !ok {
			resave = true
			c[i] = def[i]
		}
	}
	return resave
}
