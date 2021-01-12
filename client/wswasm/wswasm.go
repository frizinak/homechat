// +build js,wasm

package wswasm

import (
	"errors"
	"fmt"
	"sync"
	"syscall/js"
	"time"

	"github.com/frizinak/homechat/client"
)

type Conn struct {
	sem    sync.Mutex
	conn   js.Value
	closed bool
	err    error
	buf    [][]byte

	binary     bool
	uint8array js.Value
}

func (c *Conn) onClose() {
	if c.err == nil {
		c.err = errors.New("closed")
	}
	c.closed = true
}

func newConn(global js.Value, uri string, binary bool) (*Conn, error) {
	conn := global.Get("WebSocket").New(uri)
	if binary {
		conn.Set("binaryType", "arraybuffer")
	}

	c := &Conn{conn: conn, buf: make([][]byte, 0, 1000), binary: binary}
	c.uint8array = global.Get("Uint8Array")

	open := make(chan struct{}, 1)
	err := make(chan error, 1)
	onopen := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		open <- struct{}{}
		return nil
	})
	onclose := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		c.onClose()
		return nil
	})
	onerror := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		err <- errors.New("unknown websocket error")
		return nil
	})

	onmsg := func(data js.Value) {
		d := []byte(data.String())
		c.sem.Lock()
		c.buf = append(c.buf, d)
		c.sem.Unlock()
	}

	if binary {
		onmsg = func(data js.Value) {
			l := data.Get("byteLength").Int()
			buf := make([]byte, l)
			uint8array := c.uint8array.New(data)
			js.CopyBytesToGo(buf, uint8array)
			c.sem.Lock()
			c.buf = append(c.buf, buf)
			c.sem.Unlock()
		}
	}

	onmessage := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		onmsg(args[0].Get("data"))
		return nil
	})

	conn.Call("addEventListener", "open", onopen)
	conn.Call("addEventListener", "close", onclose)
	conn.Call("addEventListener", "error", onerror)
	conn.Call("addEventListener", "message", onmessage)
	select {
	case <-open:
		return c, nil
	case err := <-err:
		return nil, err
	}
}

func (c *Conn) Write(b []byte) (int, error) {
	if c.closed {
		return 0, c.err
	}

	if c.binary {
		val := c.uint8array.New(len(b))
		js.CopyBytesToJS(val, b)
		c.conn.Call("send", val)
		return len(b), nil
	}

	c.conn.Call("send", string(b))
	return len(b), nil
}

func (c *Conn) Read(b []byte) (int, error) {
	for {
		if c.closed {
			return 0, c.err
		}
		c.sem.Lock()
		if len(c.buf) == 0 {
			c.sem.Unlock()
			time.Sleep(time.Millisecond * 10)
			continue
		}

		n := copy(b, c.buf[0])
		c.buf[0] = c.buf[0][n:]
		if len(c.buf[0]) == 0 {
			c.buf = c.buf[1:]
		}
		c.sem.Unlock()
		return n, nil
	}
}

func (c *Conn) Close() error {
	if c.closed {
		return c.err
	}
	return nil
}

type Client struct {
	global js.Value
	uri    string
	binary bool
}

type Config struct {
	TLS    bool
	Domain string
	Path   string
	Binary bool
}

func New(c Config, global js.Value) (*Client, error) {
	schemeWS := "wss"
	if !c.TLS {
		schemeWS = "ws"
	}
	uri := fmt.Sprintf("%s://%s/%s", schemeWS, c.Domain, c.Path)
	return &Client{global: global, uri: uri, binary: c.Binary}, nil
}

func (c *Client) Connect() (client.Conn, error) {
	return newConn(c.global, c.uri, c.binary)
}
