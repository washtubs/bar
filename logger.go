package main

import (
	logger "log"
	"os"
)

var log *logger.Logger

func init() {
	f, err := os.Create("/tmp/bar-log.txt")
	if err != nil {
		panic(err)
	}

	log = logger.New(f, "", logger.Ltime|logger.Lshortfile)
}
