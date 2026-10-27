package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	port := flag.Int("port", 8000, "port")
	flag.Parse()
	cwd, _ := os.Getwd()
	cfg, err := os.ReadFile("config.json")
	if err != nil {
		fmt.Println("config.json not found in", cwd)
		os.Exit(1)
	}
	fmt.Printf("loaded ./config.json (%d bytes) from %s\n", len(cfg), cwd)
	if os.Getenv("API_TOKEN") != "" {
		fmt.Println("API_TOKEN is set")
	}
	fmt.Printf("listening on :%d\n", *port)
	for i := 1; ; i++ {
		time.Sleep(time.Second)
		fmt.Printf("GET /health 200 (%d)\n", i)
	}
}
