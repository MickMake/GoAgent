package main

import (
	"log"
	"os"
)

func init() {
	if len(os.Args) < 2 || os.Args[1] != "skill" {
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if err := runSkillCommand(cfg, os.Args[2:]); err != nil {
		log.Fatal(err)
	}
	os.Exit(0)
}
