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
	}
}

func (c *Config) Decode(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(c); err != nil {
		return fmt.Errorf("Failed to parse client config %s: %w", file, err)
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
	resave := false
	if c.NotifyCommand == nil {
		resave = true
		c.NotifyCommand = def.NotifyCommand
	}
	if c.NotifyWhen == "" {
		resave = true
		c.NotifyWhen = def.NotifyWhen
	}
	if c.ServerAddress == "" {
		resave = true
		c.ServerAddress = def.ServerAddress
	}
	if c.ServerTCPAddress == "" {
		resave = true
		c.ServerTCPAddress = def.ServerTCPAddress
	}
	if c.MaxMessages <= 0 {
		resave = true
		c.MaxMessages = def.MaxMessages
	}
	if c.MusicDownloads == "" {
		resave = true
		c.MusicDownloads = def.MusicDownloads
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
