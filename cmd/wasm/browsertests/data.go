package browsertests

import (
	"bytes"
	"encoding/json"
	"syscall/js"
	"testing"
	"time"

	"github.com/frizinak/homechat/server/channel/chat/data"
)

func BenchmarkJsonEncDec(b *testing.B) {
	msg := data.ServerMessage{}
	msg.Data = "lorem ipsum"
	msg.Notify = true
	msg.Stamp = time.Now()
	buf := bytes.NewBuffer(nil)
	var err error
	for n := 0; n < b.N; n++ {
		if err = msg.JSON(buf); err != nil {
			panic(err)
		}
		mp := make(map[string]interface{})
		if err = json.NewDecoder(buf).Decode(&mp); err != nil {
			panic(err)
		}
		js.ValueOf(mp)
		buf.Reset()
	}
}

func BenchmarkManual(b *testing.B) {
	msg := data.ServerMessage{}
	msg.Data = "lorem ipsum"
	msg.Notify = true
	msg.Stamp = time.Now()
	constructor := js.Global().Get("Object")
	obj := constructor.New()
	for n := 0; n < b.N; n++ {
		obj.Set("d", msg.Data)
		obj.Set("from", msg.From)
		obj.Set("pm", msg.PM)
		obj.Set("notify", msg.Notify)
		obj.Set("bot", msg.Bot)
		obj.Set("stamp", msg.Stamp.String())
	}
}
