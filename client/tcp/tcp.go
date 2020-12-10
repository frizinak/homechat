package tcp

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/frizinak/homechat/client"
)

type Client struct {
	tcpAddr string
}

type Config struct {
	Domain string
}

func New(c Config) (*Client, error) {
	client := &Client{}
	addr := strings.Split(c.Domain, ":")
	port, err := strconv.Atoi(addr[1])
	if err != nil {
		return nil, err
	}
	port++

	client.tcpAddr = fmt.Sprintf("%s:%d", addr[0], port)

	return client, nil
}

func (c *Client) Connect() (client.Conn, error) {
	return net.Dial("tcp", c.tcpAddr)
}
