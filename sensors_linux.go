/*
    sdlab - ScratchDuino Laboratory core daemon
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
	"fmt"
	"github.com/scratchduino/i2c"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type DataRange struct{ Min, Max float64 }

type Bus int

const (
	W1  = Bus(iota)
	I2C = Bus(iota)
)

type Device struct {
	Bus    Bus
	Id     uint
	Driver string
}

type ValueType int

const (
	GAUGE = ValueType(iota)
	COUNTER
	DERIVE
	ABSOLUTE
)

type Value struct {
	Name       string
	Range      DataRange
	Resolution time.Duration
	File       string
	Command    string
	Re         *regexp.Regexp
	Multiplier float64
	Addend     float64
	Type       ValueType
}

type Sensor struct {
	Name   string
	Values []Value
	Device Device
}

type PluggedSensor struct {
	Address uint64
	*Sensor
}

type PluggedSensors map[string]*PluggedSensor

func (bus Bus) String() string {
	switch bus {
	case W1:
		return "w1"
	case I2C:
		return "i2c"
	}
	return ""
}

func busFromString(str string) (Bus, error) {
	switch strings.ToLower(str) {
	case "w1", "1wire", "1-wire":
		return W1, nil
	case "i2c", "iic", "twi":
		return I2C, nil
	}
	return Bus(-1), errors.New("wrong bus: `" + str + "'")
}

func (typ ValueType) String() string {
	switch typ {
	case GAUGE:
		return "GAUGE"
	case COUNTER:
		return "COUNTER"
	case DERIVE:
		return "DERIVE"
	case ABSOLUTE:
		return "ABSOLUTE"
	}
	return ""
}

func valueTypeFromString(str string) (ValueType, error) {
	switch strings.ToLower(str) {
	case "gauge":
		return GAUGE, nil
	case "counter":
		return COUNTER, nil
	case "derive":
		return DERIVE, nil
	case "absolute":
		return ABSOLUTE, nil
	}
	return ValueType(-1), errors.New("wrong value type: " + str)
}

func detachI2C(bus uint, addr uint) error {
	f := fmt.Sprintf("/sys/bus/i2c/devices/i2c-%d/delete_device", bus)
	file, err := os.OpenFile(f, os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	_, err = file.Write([]byte(fmt.Sprintf("0x%x\n", addr)))
	if err != nil {
		logger.Print(err)
		return err
	}
	return file.Close()
}

func attachI2C(bus uint, addr uint, dev string) error {
	f, err := os.OpenFile(
		fmt.Sprintf("/sys/bus/i2c/devices/i2c-%d/new_device", bus),
		os.O_WRONLY, 0,
	)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%s 0x%x\n", dev, addr)
	f.Close()
	return err
}

func probeI2C(bus uint, addr uint) (bool, error) {
	dev := i2c.NewSlave(bus, addr)
	if err := dev.Open(os.O_RDONLY); err != nil {
		return false, err
	}
	_, err := dev.ReadByte()
	dev.Close()
	if err != nil {
		return false, nil
	}
	return true, nil
}

// Search performs lookup for connected sensors of receiver class and returns
// a pointer to slice of PluggedSensor and error if any.
func (sensor Sensor) Search() (PluggedSensors, error) {
	switch sensor.Device.Bus {
	case W1:
		// several 1-Wire sensors of the same type can be conected
		// simulteneously
		pattern := fmt.Sprintf("/sys/bus/w1/devices/%x-*", sensor.Device.Id)
		found, err := filepath.Glob(pattern)
		if err != nil {
			err = fmt.Errorf(
				"Can not expand glob `%s': %s",
				pattern, err,
			)
			return nil, err
		}
		detected := make(PluggedSensors, len(found))
		for i := range found {
			var addr, typ uint64
			// get device type and address from path
			n, e := fmt.Sscanf(
				found[i],
				"/sys/bus/w1/devices/%x-%x",
				&typ, &addr,
			)
			if n != 2 || e != nil {
				logger.Panic("Error parsing 1-Wire slave file name `" + found[i] + "'")
			}
			addr = addr << 8
			addr = addr | typ
			id := fmt.Sprintf("%s-%x", sensor.Name, addr)
			logger.Printf("Detected 1-Wire sensor %s (type 0x%x) with address 0x%x; assigned ID %s",
				sensor.Name, typ, addr, id)
			detected[id] = &PluggedSensor{addr, &sensor}
		}
		return detected, nil
	case I2C:
		// only one i2c sensor with given address can be connected
		// to single bus
		detected := make(PluggedSensors, len(config.I2C.Buses))
		for i := range config.I2C.Buses {
			if f, err := os.Open(
				fmt.Sprintf("/sys/bus/i2c/devices/i2c-%d/%x-%04x/name",
					config.I2C.Buses[i],
					config.I2C.Buses[i],
					sensor.Device.Id,
				)); err == nil {
				f.Close()
				// delete the device
				// until we ensure it is actually connected
				err = detachI2C(config.I2C.Buses[i], sensor.Device.Id)
				if err != nil {
					logger.Print(err)
				}
			}
			found, err := probeI2C(config.I2C.Buses[i], sensor.Device.Id)
			if err != nil {
				logger.Print(err)
			}
			if !found {
				continue
			}
			// device is connected
			addr := (uint64(config.I2C.Buses[i]) << 8) | uint64(sensor.Device.Id)
			if sensor.Device.Driver != "" {
				err = attachI2C(
					config.I2C.Buses[i],
					sensor.Device.Id,
					sensor.Device.Driver,
				)
				if err != nil {
					logger.Print(err)
					continue
				}
			}
			id := fmt.Sprintf("%s-%x:%x",
				sensor.Name, config.I2C.Buses[i], sensor.Device.Id)
			detected[id] = &PluggedSensor{addr, &sensor}
			logger.Printf("Detected I2C sensor %s at bus 0x%x, address 0x%x; assigned ID %s\n",
				sensor.Name, config.I2C.Buses[i], sensor.Device.Id, id,
			)
		}
		return detected, nil
	}
	return nil, errors.New("Unknown sensor type")
}

// GetData reads n'th value from sensor and returns it and error, if any.
func (sensor PluggedSensor) GetData(n int) (data float64, err error) {
	var s []byte
	if sensor.Values[n].Command != "" {
		var cmd string
		switch sensor.Device.Bus {
		case W1:
			typ := sensor.Address & 0xff
			addr := sensor.Address >> 8
			cmd = strings.Replace(sensor.Values[n].Command, "${typ}", fmt.Sprintf("%d", typ), -1)
			cmd = strings.Replace(cmd, "${addr}", fmt.Sprintf("%d", addr), -1)
		case I2C:
			addr := sensor.Address & 0xff
			bus := sensor.Address >> 8
			cmd = strings.Replace(sensor.Values[n].Command, "${bus}", fmt.Sprintf("%d", bus), -1)
			cmd = strings.Replace(cmd, "${addr}", fmt.Sprintf("%d", addr), -1)
		default:
			logger.Panic("unknown bus")
		}
		c := exec.Command("/bin/sh", "-c", cmd)
		s, err = c.Output()
		if err != nil {
			logger.Print("`" + cmd + "': " + err.Error())
		}
	} else if path.IsAbs(sensor.Values[n].File) {
		// read value from file by absolute path.
		// mainly for debugging.
		s, err = ioutil.ReadFile(sensor.Values[n].File)
		if err != nil {
			err = fmt.Errorf(
				"Can not read file `%s': %s",
				sensor.Values[n].File, err,
			)
			return 0.0, err
		}
	} else {
		// read value from file relative to device directory in sysfs
		var file string
		switch sensor.Device.Bus {
		case W1:
			typ := sensor.Address & 0xff
			addr := sensor.Address >> 8
			file = fmt.Sprintf("/sys/bus/w1/devices/%x-%012x/", typ, addr)
			if sensor.Values[n].File == "" {
				// default file name
				file += "w1_slave"
			} else {
				// custom file name
				file += sensor.Values[n].File
			}
		case I2C:
			if sensor.Values[n].File == "" {
				return 0.0, errors.New("No file nor command specified")
			}
			addr := sensor.Address & 0xff
			bus := sensor.Address >> 8
			file = fmt.Sprintf("/sys/bus/i2c/devices/i2c-%d/%x-%04x/%s", bus, bus, addr, sensor.Values[n].File)
		default:
			logger.Panic("unknown bus")
		}
		s, err = ioutil.ReadFile(file)
		if err != nil {
			err = fmt.Errorf("Car not read file `%s': %s", file, err)
			return 0.0, err
		}
	}
	strdata := sensor.Values[n].Re.FindSubmatch(s)
	switch len(strdata) {
	case 0:
		return 0.0, errors.New("No data received")
	case 1:
		data, err = strconv.ParseFloat(string(strdata[0]), 64)
	default:
		data, err = strconv.ParseFloat(string(strdata[1]), 64)
	}
	if err != nil {
		return math.NaN(), errors.New("Can not parse data: " + err.Error())
	}
	data = data*sensor.Values[n].Multiplier + sensor.Values[n].Addend
	return data, nil
}

func scanSensors() (err error) {
	logger.Print("Searching for sensors...")
	pluggedSensors = make(PluggedSensors)
	for i := range sensors {
		found, err := sensors[i].Search()
		if err != nil {
			return err
		}
		for id := range found {
			pluggedSensors[id] = found[id]
		}
	}
	return nil
}

// valueAvailable take sensor ID and value index and returns true if such
// a sensor exists and has a value with such index, or false otherwise.
func valueAvailable(s string, v int) bool {
	_, ok := pluggedSensors[s]
	if !ok {
		return false
	}
	if v >= len(pluggedSensors[s].Values) || v < 0 {
		return false
	}
	return true
}
