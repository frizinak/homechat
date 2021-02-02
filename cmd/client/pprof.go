// +build pprof

package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
)

func init() {
	fmt.Println("pprof build! (:6061)")
	go func() {
		if err := http.ListenAndServe(":6061", nil); err != nil {
			fmt.Printf("failed to start pprof http server: %s\n", err)
		}
	}()
}
