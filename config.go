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
	"github.com/scratchduino/sdlab/user"
	"gopkg.in/yaml.v1"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type User int

type Group int

type SocketConf struct {
	Enable bool
	Path   string
	User   *User
	Group  *Group
	Mode   os.FileMode
}

type TCPConf struct {
	Enable bool
	Listen string
}

type I2CConf struct {
	Buses []uint
}

type SeriesConf struct {
	Buffer uint
}

type MonitorConf struct {
	Path string
}

type Config struct {
	Socket      SocketConf
	TCP         TCPConf
	SensorsPath string
	I2C         I2CConf
	Series      SeriesConf
	Monitor     MonitorConf
	Log         string
}

type DeviceYAML struct {
	Bus    string
	Id     uint
	Driver string
}

type ValueYAML struct {
	Name       string
	Range      DataRange
	Resolution int
	File       string ",omitempty"
	Command    string ",omitempty"
	Re         string
	Multiplier float64
	Addend     float64 ",omitempty"
	Type       ValueType
}

type SensorYAML struct {
	Name   string
	Values []ValueYAML
	Device DeviceYAML
}

var config Config

func (uid *User) SetYAML(tag string, username interface{}) bool {
	s, ok := username.(string)
	if !ok {
		return false
	}
	u, err := user.Lookup(s)
	if err != nil {
		logger.Print(err)
		return false
	}
	*uid = User(u.Uid)
	return true
}

func (gid *Group) SetYAML(tag string, groupname interface{}) bool {
	s, ok := groupname.(string)
	if !ok {
		return false
	}
	g, err := user.LookupGroup(s)
	if err != nil {
		logger.Print(err)
		return false
	}
	*gid = Group(g.Gid)
	return true
}

func valuesFromYAML(valuesYAML []ValueYAML) (values []Value, err error) {
	values = make([]Value, len(valuesYAML))
	for i := range valuesYAML {
		value, err := valueFromYAML(valuesYAML[i])
		values[i] = *value
		if err != nil {
			return values, err
		}
	}
	return values, nil
}

func valueFromYAML(valueYAML ValueYAML) (value *Value, err error) {
	var re *regexp.Regexp
	var multiplier float64
	if valueYAML.Re == "" {
		re = regexp.MustCompile(".*")
	} else {
		re, err = regexp.Compile(valueYAML.Re)
		if err != nil {
			logger.Printf("Error compilng regexp `%s': %s", valueYAML.Re, err)
		}
	}
	if math.Abs(valueYAML.Multiplier) > math.SmallestNonzeroFloat64 {
		multiplier = valueYAML.Multiplier
	} else {
		multiplier = 1
	}
	value = &Value{
		valueYAML.Name,
		valueYAML.Range,
		time.Duration(valueYAML.Resolution) * time.Millisecond,
		valueYAML.File,
		valueYAML.Command,
		re,
		multiplier,
		valueYAML.Addend,
		valueYAML.Type,
	}
	return value, err
}

func deviceFromYAML(deviceYAML DeviceYAML) (device *Device, err error) {
	bus, err := busFromString(deviceYAML.Bus)
	device = &Device{
		bus,
		deviceYAML.Id,
		deviceYAML.Driver,
	}
	return device, err
}

func sensorFromYAML(sensorYAML SensorYAML) (sensor *Sensor, err error) {
	values, err := valuesFromYAML(sensorYAML.Values)
	if err != nil {
		return nil, err
	}
	device, err := deviceFromYAML(sensorYAML.Device)
	if err != nil {
		return nil, err
	}
	sensor = &Sensor{
		sensorYAML.Name,
		values,
		*device,
	}
	return sensor, nil
}

func loadSensors(path string) (err error) {
	files, err := filepath.Glob(config.SensorsPath + "/*.yml")
	if err != nil {
		return err
	}
	sensors = make([]Sensor, 0, len(files))
	for i := range files {
		yml, err := ioutil.ReadFile(files[i])
		if err != nil {
			logger.Printf("Error reading file `%s': %s", files[i], err)
			continue
		}
		var sensorYAML SensorYAML
		err = yaml.Unmarshal(yml, &sensorYAML)
		if err != nil {
			logger.Printf("Error parsing file `%s': %s", files[i], err)
			continue
		}
		sensor, err := sensorFromYAML(sensorYAML)
		if err != nil {
			logger.Printf("Error reading gonfiguration from file `%s': %s", files[i], err)
			continue
		}
		sensors = append(sensors, *sensor)
	}
	return nil
}

func loadConfig(path string) (err error) {
	yml, err := ioutil.ReadFile(path)
	if err != nil {
		logger.Printf("Error reading file `%s': %s", path, err)
	}
	err = yaml.Unmarshal(yml, &config)
	if config.Socket.Path == "" {
		config.Socket.Path = "/run/sdlab.sock"
	}
	if config.Socket.Mode == 0 {
		config.Socket.Mode = 0777
	}
	if config.SensorsPath == "" {
		config.SensorsPath = "/etc/sdlab/sensors.d"
	}
	if config.Series.Buffer == 0 {
		config.Series.Buffer = 100
	}
	if config.Monitor.Path == "" {
		config.Monitor.Path = "/var/lib/sdlab/monitor"
	}
	return err
}
