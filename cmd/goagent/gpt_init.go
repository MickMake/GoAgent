package main

import (
	"log"
	"os"
)

func init() {
	if len(os.Args) < 2 || os.Args[1] != "gpt" {
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if err := runGPTCommand(cfg, os.Args[2:]); err != nil {
		log.Fatal(err)
	}
	os.Exit(0)
}
