package main

import (
	"github.com/go-bindata/go-bindata"
)

func main() {
	input := bindata.InputConfig{"public", true}
	fn := "bound/bound.go"
	c := bindata.NewConfig()
	c.Package = "bound"
	c.Output = fn
	c.Input = []bindata.InputConfig{input}
	c.Prefix = "public"
	c.NoMetadata = true
	if err := bindata.Translate(c); err != nil {
		panic(err)
	}
}
