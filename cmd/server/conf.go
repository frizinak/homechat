package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type Config struct {
	Directory                 string
	YMDir                     string
	ChatMessagesAppendOnlyDir *string

	HTTPAddr string
	TCPAddr  string

	BandwidthIntervalSeconds *int
	MaxUploadKBytes          *int64
	MaxChatMessages          int

	WttrCity           string
	HolidayCountryCode string

	HueIP   string
	HuePass string
}

func (c *Config) Help(w io.Writer) error {
	buf := bytes.NewBuffer(nil)
	buf.WriteString("Directory:                 Data directory\n")
	buf.WriteString("                           Location of all channel logs\n")
	buf.WriteString("\n")
	buf.WriteString("YMDir:                     Libym data directory\n")
	buf.WriteString("                           Location of songs and song database\n")
	buf.WriteString("\n")
	buf.WriteString("ChatMessagesAppendOnlyDir: Location of append only chat logs\n")
	buf.WriteString("                           Empty to not store any logs (!)\n")
	buf.WriteString("\n")
	buf.WriteString("HTTPAddr:                  ip:port of the http server\n")
	buf.WriteString("                           Should be an actual ip, 192.168.0.1:1200\n")
	buf.WriteString("                           not 0.0.0.0:1200\n")
	buf.WriteString("\n")
	buf.WriteString("TCPAddr:                   ip:port of the tcp server\n")
	buf.WriteString("                           Can be 0.0.0.0:1201\n")
	buf.WriteString("\n")
	buf.WriteString("BandwidthIntervalSeconds:  Log bandwidth usage every n seconds\n")
	buf.WriteString("                           0 for no logging\n")
	buf.WriteString("\n")
	buf.WriteString("MaxUploadKBytes:           Maximum file upload size in KiB\n")
	buf.WriteString("\n")
	buf.WriteString("MaxChatMessages:           Maximum amount of chat messages\n")
	buf.WriteString("                           that are kept in the main channel logfile\n")
	buf.WriteString("                           (!) note: the logfile will be truncated to this size\n")
	buf.WriteString("\n")
	buf.WriteString("WttrCity:                  Name of the city to be used as\n")
	buf.WriteString("                           the default for wttr.in bot\n")
	buf.WriteString("\n")
	buf.WriteString("HolidayCountryCode:        2-letter country code to be used as\n")
	buf.WriteString("                           the default for the \n")
	buf.WriteString("                           https://date.nager.at/api/v2/PublicHolidays bot\n")
	buf.WriteString("\n")
	buf.WriteString("HueIP:                     Philips Hue bridge ip for the hue bot\n")
	buf.WriteString("\n")
	buf.WriteString("HuePass:                   Philips Hue bridge pass for the hue bot\n")
	buf.WriteString("                           You can use https://github.com/amimof/huego\n")
	buf.WriteString("                           or https://github.com/frizinak/hue or ...\n")
	buf.WriteString("                           to find the ip and generate a password\n")
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
	if c.MaxChatMessages <= 0 {
		resave = true
		c.MaxChatMessages = def.MaxChatMessages
	}
	if c.ChatMessagesAppendOnlyDir == nil {
		resave = true
		c.ChatMessagesAppendOnlyDir = def.ChatMessagesAppendOnlyDir
	}
	if c.TCPAddr == "" {
		resave = true
		c.TCPAddr = def.TCPAddr
	}
	return resave
}
