package tcp

import (
	"net"

	"github.com/frizinak/homechat/client"
)

type Client struct {
	tcpAddr string
}

type Config struct {
	TCPAddr string
}

func New(c Config) *Client { return &Client{c.TCPAddr} }

func (c *Client) Connect() (client.Conn, error) {
	return net.Dial("tcp", c.tcpAddr)
}

func (c *Client) Framed() bool { return false }
