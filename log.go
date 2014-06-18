package main

import (
	"errors"
	"log"
	"os"
)

// openLog opens log file specified in config or stderr if nothing specified.
// It returns logger and error, if any.
func openLog() (*log.Logger, error) {
	if config.Log != "" {
		// open or create log file specified in config
		f, err := os.OpenFile(config.Log, os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			if os.IsNotExist(err) {
				f, err = os.Create(config.Log)
				if err != nil {
					err = errors.New("Error creating log file: " + err.Error())
					return nil, err
				}
			} else {
				err = errors.New("Error opening log file: " + err.Error())
				return nil, err
			}
		}
		defer f.Close()
		logger = log.New(f, "", log.LstdFlags)
	} else {
		// path to log file not specified, use stderr
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	return logger, nil
}
