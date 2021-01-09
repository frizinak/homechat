package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	Directory string
	HTTPAddr  string
	YMDir     string

	BandwidthIntervalSeconds *int
	MaxUploadKBytes          *int64

	WttrCity           string
	HolidayCountryCode string

	HueIP   string
	HuePass string
}

func (c *Config) Decode(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(c); err != nil {
		return fmt.Errorf("Failed to parse server config %s: %w", file, err)
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
	if c.WttrCity == "" {
		resave = true
		c.WttrCity = def.WttrCity
	}
	if c.HolidayCountryCode == "" {
		resave = true
		c.HolidayCountryCode = def.HolidayCountryCode
	}
	if c.YMDir == "" {
		resave = true
		c.YMDir = def.YMDir
	}
	if c.Directory == "" {
		resave = true
		c.Directory = def.Directory
	}
	if c.HTTPAddr == "" {
		resave = true
		c.HTTPAddr = def.HTTPAddr
	}
	if c.BandwidthIntervalSeconds == nil {
		resave = true
		c.BandwidthIntervalSeconds = def.BandwidthIntervalSeconds
	}
	if c.MaxUploadKBytes == nil {
		resave = true
		c.MaxUploadKBytes = def.MaxUploadKBytes
	}
	return resave
}
