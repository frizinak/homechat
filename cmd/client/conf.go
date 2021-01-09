package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type NotifyWhen string

const (
	NotifyDefault = "default"
	NotifyAlways  = "always"
)

type Config struct {
	NotifyCommand *string
	NotifyWhen    string
	ServerAddress string
	Username      string
	MaxMessages   int
}

func (c *Config) Help(w io.Writer) error {
	buf := bytes.NewBuffer(nil)
	buf.WriteString("NotifyCommand: program to run to show a notification\n")
	buf.WriteString("               %u will be replaced with the username of the sender\n")
	buf.WriteString("               %m will be replaced with the message\n")
	buf.WriteString("               example: notify-send 'Homechat' '%u: %m'\n")
	buf.WriteString("\n")
	buf.WriteString("NotifyWhen:    When to trigger the above command\n")
	buf.WriteString("               one off 'default' or 'always'\n")
	buf.WriteString("               default: direct messages or chat messages that start with '!'\n")
	buf.WriteString("               always:  well... always\n")
	buf.WriteString("\n")
	buf.WriteString("ServerAddress: ip:port of the server\n")
	buf.WriteString("\n")
	buf.WriteString("Username:      your desired username\n")
	buf.WriteString("\n")
	buf.WriteString("MaxMessages:   maximum amount of messages shown\n")
	buf.WriteString("\n")
	_, err := io.Copy(w, buf)
	return err
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
	if c.MaxMessages <= 0 {
		resave = true
		c.MaxMessages = def.MaxMessages
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
