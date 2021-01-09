package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	NotifyCommand *string
	ServerAddress string
	Username      string
	MaxMessages   int
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
