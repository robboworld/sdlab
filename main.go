package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

var configPath string
var logger *log.Logger
var sensors []Sensor
var pluggedSensors PluggedSensors

func init() {
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	} else {
		configPath = "/etc/sdlab/sdlab.conf"
	}
}

func main() {
	err := loadConfig(configPath)
	if err != nil {
		// logger is nil ATM
		log.Fatal("Error loading configuration: %s", err)
	}
	logger, err = openLog()
	if err != nil {
		log.Fatal(err)
	}
	err = loadSensors(config.SensorsPath)
	if err != nil {
		logger.Printf("Error loading sensors configuration: %s", err)
	}
	err = scanSensors()
	if err != nil {
		logger.Fatal(err)
	}
	err = loadRunMonitors()
	if err != nil {
		logger.Print("Error running monitor: " + err.Error())
	}
	listeners, err := startAPI()
	if err != nil {
		logger.Fatal(err)
	}
	if len(listeners) == 0 {
		logger.Fatal("No interfaces started")
	}
	for i := range listeners {
		defer listeners[i].Close()
	}
	terminate := make(chan os.Signal, 1)
	signal.Notify(terminate, syscall.SIGINT, syscall.SIGTERM)
	for sig := range terminate {
		logger.Printf("caught %v signal, exiting", sig)
		for i := range listeners {
			listeners[i].Close()
		}
		os.Exit(0)
	}
}
