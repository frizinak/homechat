package ws

import (
	"github.com/frizinak/homechat/client"
	"golang.org/x/net/websocket"
)

type Client struct {
	c *websocket.Config
}

type Config struct {
	TLS    bool
	Domain string
	Path   string
}

func New(c Config) (*Client, error) {
	client := &Client{}

	schemeWS := "wss"
	scheme := "https"
	if !c.TLS {
		schemeWS = "ws"
		scheme = "http"
	}

	wsc, err := websocket.NewConfig(schemeWS+"://"+c.Domain+"/"+c.Path, scheme+"://"+c.Domain)
	if err != nil {
		return nil, err
	}

	// todo
	// wsc.TlsConfig = tlsconf

	client.c = wsc

	return client, nil
}

func (c *Client) Connect() (client.Conn, error) {
	return websocket.DialConfig(c.c)
}
