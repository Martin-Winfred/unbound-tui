package main

import "flag"

func main() {
	configPath := flag.String("config", "", "path to the SQLite database")
	flag.Parse()

	if *configPath == "" {
		return
	}
}
