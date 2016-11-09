/*
    sdlab - STEM Lab core daemon
    Copyright (C) 2014  Dmitry Mikhirev <mikhirev@mezon.ru>

    This program is free software: you can redistribute it and/or modify
    it under the terms of the GNU General Public License as published by
    the Free Software Foundation, either version 3 of the License, or
    (at your option) any later version.

    This program is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU General Public License for more details.

    You should have received a copy of the GNU General Public License
    along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

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
	var err error

	err = loadConfig(configPath)
	if err != nil {
		// logger still is nil
		log.Fatalf("Error loading configuration: %s", err)
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

	// Database prepare

	db, err = initDB(config.Database)
	if err != nil {
		logger.Fatal(err)
	}
	defer db.Close()

	err = initQueries(config.Database.Type)
	defer cleanupQueries()
	if err != nil {
		logger.Fatal(err)
	}

	err = prepareDB()
	if err != nil {
		logger.Fatal(err)
	}
	logger.Print("Database connected")

	// Run monitors

	err = loadRunMonitors()
	if err != nil {
		logger.Print("Error running monitor: " + err.Error())
	}

	// Start API and listeners

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
