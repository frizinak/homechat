// +build pprof

package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
)

func init() {
	fmt.Println("pprof build!")
	go func() {
		if err := http.ListenAndServe(":6060", nil); err != nil {
			fmt.Printf("failed to start pprof http server: %s\n", err)
		}
	}()
}
