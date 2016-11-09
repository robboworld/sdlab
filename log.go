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
		logger = log.New(f, "", log.LstdFlags)
	} else {
		// path to log file not specified, use stderr
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	return logger, nil
}
