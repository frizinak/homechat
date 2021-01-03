package main

import (
	"bytes"
	"encoding/json"
	"syscall/js"

	"github.com/frizinak/homechat/client"
	"github.com/frizinak/homechat/client/wswasm"
	"github.com/frizinak/homechat/server/channel"
	chatdata "github.com/frizinak/homechat/server/channel/chat/data"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
	usersdata "github.com/frizinak/homechat/server/channel/users/data"
	"github.com/frizinak/homechat/vars"
)

type handler string

const (
	OnName              handler = "onName"
	OnChatMessage       handler = "onChatMessage"
	OnMusicMessage      handler = "onMusicMessage"
	OnMusicStateMessage handler = "onMusicStateMessage"
	OnUsersMessage      handler = "onUsersMessage"
	OnLog               handler = "onLog"
	OnFlash             handler = "onFlash"
	OnError             handler = "onError"
)

type jsHandler struct {
	handler  js.Value
	handlers map[handler]js.Value
	buf      *bytes.Buffer
	name     string
}

func newJSHandler(h js.Value) *jsHandler {
	methods := []handler{
		OnName,
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

func (j *jsHandler) HandleChatMessage(m chatdata.ServerMessage) error {
	return j.on(OnChatMessage, m)
}

func (j *jsHandler) HandleMusicMessage(m musicdata.ServerMessage) error {
	return j.on(OnMusicMessage, m)
}

func (j *jsHandler) HandleMusicStateMessage(m musicdata.ServerStateMessage) error {
	return j.on(OnMusicStateMessage, m)
}

func (j *jsHandler) HandleUsersMessage(m usersdata.ServerMessage) error {
	return j.on(OnUsersMessage, m)
}

func (j *jsHandler) Log(s string)   { j.handlers[OnLog].Invoke(s) }
func (j *jsHandler) Err(s string)   { j.handlers[OnError].Invoke(s) }
func (j *jsHandler) Flash(s string) { j.handlers[OnFlash].Invoke(s) }

func main() {
	window := js.Global()
	console := window.Get("console")
	document := window.Get("document")
	location := document.Get("location")
	host := location.Get("host").String()
	httpProto := location.Get("protocol").String()

	public := window.Get("Object").New()
	window.Set("homechat", public)

	backendConf := wswasm.Config{
		TLS:    httpProto == "https:",
		Domain: host,
		Path:   "ws",
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

	public.Set("init", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) != 1 {
			console.Call("error", "init requires 1 arg")
			return nil
		}
		handler = newJSHandler(args[0])
		conf := client.Config{
			ServerURL: httpProto + "//" + host,
			Name:      args[0].Get("name").String(),
			Channels: []string{
				vars.UserChannel,
				vars.ChatChannel,
				vars.MusicChannel,
				vars.MusicStateChannel,
				vars.MusicErrorChannel,
			},
			Proto:   channel.ProtoJSON,
			Framed:  true,
			History: true,
		}
		c = client.New(backend, handler, handler, conf)

		public.Set("chat", createSender(c.Chat))
		public.Set("music", createSender(c.Music))

		go func() {
			if err := c.Run(); err != nil {
				panic(err)
			}
		}()

		return nil
	}))

	ch := make(chan struct{})
	<-ch
}
