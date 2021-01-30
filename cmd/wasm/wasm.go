// +build js,wasm

package main

import (
	"bytes"
	"encoding/json"
	"syscall/js"
	"time"

	"github.com/frizinak/homechat/client"
	"github.com/frizinak/homechat/client/backend/wswasm"
	noop "github.com/frizinak/homechat/client/handler"
	"github.com/frizinak/homechat/crypto"
	"github.com/frizinak/homechat/server/channel"
	chatdata "github.com/frizinak/homechat/server/channel/chat/data"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
	usersdata "github.com/frizinak/homechat/server/channel/users/data"
	"github.com/frizinak/homechat/vars"
)

type handler string

const (
	OnName              handler = "onName"
	OnHistory           handler = "onHistory"
	OnLatency           handler = "onLatency"
	OnChatMessage       handler = "onChatMessage"
	OnMusicMessage      handler = "onMusicMessage"
	OnMusicStateMessage handler = "onMusicStateMessage"
	OnUsersMessage      handler = "onUsersMessage"
	OnLog               handler = "onLog"
	OnFlash             handler = "onFlash"
	OnError             handler = "onError"
)

type jsHandler struct {
	client.Handler
	handler  js.Value
	handlers map[handler]js.Value
	buf      *bytes.Buffer
	name     string
}

func newJSHandler(h js.Value, underlying client.Handler) *jsHandler {
	methods := []handler{
		OnName,
		OnHistory,
		OnLatency,
		OnChatMessage,
		OnMusicMessage,
		OnMusicStateMessage,
		OnUsersMessage,
		OnLog,
		OnFlash,
		OnError,
	}
	mp := make(map[handler]js.Value)
	for _, m := range methods {
		mp[m] = h.Get(string(m))
	}
	return &jsHandler{
		Handler:  underlying,
		handler:  h,
		handlers: mp,
		buf:      bytes.NewBuffer(nil),
	}
}

func (j *jsHandler) on(method handler, msg channel.Msg) error {
	// much wow
	j.buf.Reset()
	if err := msg.JSON(j.buf); err != nil {
		return err
	}
	dec := json.NewDecoder(j.buf)
	m := make(map[string]interface{})
	if err := dec.Decode(&m); err != nil {
		return err
	}

	j.handlers[method].Invoke(m)
	return nil
}

func (j *jsHandler) HandleError(err error) {
	j.handlers[OnError].Invoke(err.Error())
}

func (j *jsHandler) HandleName(name string) {
	j.name = name
	j.handlers[OnName].Invoke(name)
}

func (j *jsHandler) HandleHistory() {
	j.handlers[OnHistory].Invoke()
}

func (j *jsHandler) HandleLatency(l time.Duration) {
	j.handlers[OnLatency].Invoke(l.Milliseconds())
}

func (j *jsHandler) HandleChatMessage(m chatdata.ServerMessage) error {
	return j.on(OnChatMessage, m)
}

func (j *jsHandler) HandleMusicMessage(m musicdata.ServerMessage) error {
	return j.on(OnMusicMessage, m)
}

func (j *jsHandler) HandleMusicStateMessage(m client.MusicState) error {
	return j.on(OnMusicStateMessage, m)
}

func (j *jsHandler) HandleUsersMessage(m usersdata.ServerMessage, users client.Users) error {
	list := make([]interface{}, len(users))
	for i, u := range users {
		c := make([]interface{}, len(u.Channels))
		for i, ch := range u.Channels {
			c[i] = ch
		}
		list[i] = map[string]interface{}{
			"name":    u.Name,
			"channel": c,
			"amount":  u.Amount,
		}
	}

	j.handlers[OnUsersMessage].Invoke(list)
	return nil
}

func (j *jsHandler) Log(s string)                      { j.handlers[OnLog].Invoke(s) }
func (j *jsHandler) Err(s error)                       { j.handlers[OnError].Invoke(s.Error()) }
func (j *jsHandler) Flash(s string, dur time.Duration) { j.handlers[OnFlash].Invoke(s) }

func main() {
	window := js.Global()
	console := window.Get("console")
	document := window.Get("document")
	location := document.Get("location")
	host := location.Get("host").String()
	httpProto := location.Get("protocol").String()
	localStorage := window.Get("localStorage")

	public := window.Get("Object").New()
	window.Set("homechat", public)

	pem := localStorage.Call("getItem", "key")
	_fp := localStorage.Call("getItem", "fp")

	// TODO when server can handle tls and internal encryption is off
	// binary should depend on proto
	const binary = true
	backendConf := wswasm.Config{
		TLS:    httpProto == "https:",
		Domain: host,
		Path:   "ws",
		Binary: binary,
	}

	fp := ""
	if !_fp.IsNull() && !_fp.IsUndefined() {
		fp = _fp.String()
	}

	key := crypto.NewKey(channel.ClientMinKeySize, channel.ClientMinKeySize) // browsers
	if !pem.IsNull() && !pem.IsUndefined() {
		if err := key.UnmarshalPEM([]byte(pem.String())); err != nil {
			panic(err)
		}
	}

	err := key.Generate()
	if err == nil {
		d, err := key.MarshalPEM()
		if err != nil {
			panic(err)
		}

		localStorage.Call("setItem", "key", string(d))
	} else if err != crypto.ErrKeyExists {
		panic(err)
	}

	backend, err := wswasm.New(backendConf, window)
	if err != nil {
		panic(err)
	}

	var c *client.Client
	var handler *jsHandler

	createSender := func(sender func(string) error) js.Func {
		return js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			go func() {
				if err := sender(args[0].String()); err != nil {
					handler.HandleError(err)
				}
			}()
			return nil
		})
	}

	lastType := time.Now()
	typing := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		now := time.Now()
		if now.Sub(lastType) < time.Second*2 {
			return nil
		}
		lastType = now
		go func() {
			if err := c.ChatTyping(); err != nil {
				handler.HandleError(err)
			}
		}()
		return nil
	})

	proto := channel.ProtoJSON
	if binary {
		proto = channel.ProtoBinary
	}

	public.Set("init", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) != 1 {
			console.Call("error", "init requires 1 arg")
			return nil
		}
		handler = newJSHandler(args[0], noop.NoopHandler{})
		conf := client.Config{
			Key:               key,
			ServerFingerprint: fp,
			ServerURL:         httpProto + "//" + host,
			Name:              args[0].Get("name").String(),
			Channels: []string{
				vars.PingChannel,
				vars.UserChannel,
				vars.HistoryChannel,
				vars.ChatChannel,
				vars.TypingChannel,
				vars.MusicChannel,
				vars.MusicStateChannel,
				vars.MusicSongChannel,
				vars.MusicErrorChannel,
			},
			Proto:   proto,
			History: 100,
		}
		c = client.New(backend, handler, handler, conf)

		public.Set("chat", createSender(c.Chat))
		public.Set("music", createSender(c.Music))
		public.Set("typing", typing)

		go func() {
			err := c.Connect()
			if err == client.ErrFingerPrint {
				// TODO!!!
				newFP := c.ServerFingerprint()
				localStorage.Call("setItem", "fp", newFP)
				c.SetTrustedFingerprint(newFP)
				if err := c.Connect(); err != nil {
					panic(err)
				}
			} else if err != nil {
				panic(err)
			}
			if err := c.Run(); err != nil {
				panic(err)
			}
		}()

		return nil
	}))

	ch := make(chan struct{})
	<-ch
}
