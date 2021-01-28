package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/frizinak/homechat/server"
)

type Config struct {
	Directory                 string
	YMDir                     string
	ChatMessagesAppendOnlyDir *string

	ClientPolicy     server.ClientPolicy
	ClientPolicyFile string

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
	buf.WriteString("ClientPolicy:              Specify the client policy.\n")
	buf.WriteString("                           i.e.: a policy that determines who can connect\n")
	buf.WriteString("                           and with what username.\n")
	buf.WriteString("                           One of:\n")
	buf.WriteString(fmt.Sprintf("                             - %-8s: everyone can connect and pick an arbitrary username.\n", server.PolicyWorld))
	buf.WriteString(fmt.Sprintf("                             - %-8s: only those in the fingerprint file are allowed.\n", server.PolicyAllow))
	buf.WriteString(fmt.Sprintf("                             - %-8s: same as `fingerprint` but force name as well.\n", server.PolicyFixed))
	buf.WriteString("\n")
	buf.WriteString("ClientPolicyFile:          Location of the client policy file\n")
	buf.WriteString("                           Each line should contain exactly one fingerprint and username\n")
	buf.WriteString("                           separated by a space\n")
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
	if c.ClientPolicy == "" {
		resave = true
		c.ClientPolicy = def.ClientPolicy
	}
	if c.ClientPolicyFile == "" {
		resave = true
		c.ClientPolicyFile = def.ClientPolicyFile
	}
	return resave
}
