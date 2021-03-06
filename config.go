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
	"github.com/scratchduino/sdlab/user"
	"gopkg.in/yaml.v1"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"time"
	"fmt"
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
	Pool   uint
}

type MonitorConf struct {
	Path string
}

type DatabaseConf struct {
	Type string
	Dsn  string
}

type Config struct {
	Socket      SocketConf
	TCP         TCPConf
	SensorsPath string
	I2C         I2CConf
	Series      SeriesConf
	Monitor     MonitorConf
	Database    DatabaseConf
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
	File       string `yaml:",omitempty"`
	Command    string `yaml:",omitempty"`
	Re         string
	Multiplier float64
	Addend     float64 `yaml:",omitempty"`
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
			logger.Printf("Error compiling regexp '%s': %s", valueYAML.Re, err)
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
	files, err := filepath.Glob(path + "/*.yml")
	if err != nil {
		return err
	}
	sensors = make([]Sensor, 0, len(files))
	for i := range files {
		yml, err := ioutil.ReadFile(files[i])
		if err != nil {
			logger.Printf("Error reading file '%s': %s", files[i], err)
			continue
		}
		var sensorYAML SensorYAML
		err = yaml.Unmarshal(yml, &sensorYAML)
		if err != nil {
			logger.Printf("Error parsing file '%s': %s", files[i], err)
			continue
		}
		sensor, err := sensorFromYAML(sensorYAML)
		if err != nil {
			logger.Printf("Error reading configuration from file '%s': %s", files[i], err)
			continue
		}
		sensors = append(sensors, *sensor)
	}
	return nil
}

func loadConfig(path string) (err error) {
	yml, err := ioutil.ReadFile(path)
	if err != nil {
		errf := fmt.Errorf("Error reading file '%s': %s", path, err)
		if errf != nil {
			return errf
		}
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
	if config.Series.Pool == 0 {
		config.Series.Pool = 50
	}
	if config.Monitor.Path == "" {
		config.Monitor.Path = "/var/lib/sdlab/monitor"
	}
	if config.Database.Type == "" {
		config.Database.Type = "sqlite"
	}
	if config.Database.Dsn == "" {
		switch config.Database.Type {
		case "sqlite":
			// Format: [file:]dbname[?param1=value1&...&paramN=valueN]
			// @see https://www.sqlite.org/c3ref/open.html 
			// Query parameters:
			//   vfs:       Name of a VFS object.
			//   mode:      The mode parameter may be set to either "ro", "rw", "rwc", or "memory".
			//   cache:     The cache parameter may be set to either "shared" or "private".
			//   psow:      The psow parameter indicates whether or not the powersafe overwrite property does or 
			//              does not apply to the storage media on which the database file resides.
			//   nolock:    The nolock parameter is a boolean query parameter which if set disables file locking in rollback journal modes.
			//              This is useful for accessing a database on a filesystem that does not support locking.
			//              Caution: Database corruption might result if two or more processes write to the same database and any one of those processes uses nolock=1.
			//   immutable: The immutable parameter is a boolean query parameter that indicates that the database file is stored on read-only media.
			//              When immutable is set, SQLite assumes that the database file cannot be changed, even by a process with higher privilege,
			//              and so the database is opened read-only and all locking and change detection is disabled.
			//              Caution: Setting the immutable property on a database file that does in fact change can result 
			//              in incorrect query results and/or SQLITE_CORRUPT errors. See also: SQLITE_IOCAP_IMMUTABLE.
			config.Database.Dsn = "/data/sdlab.db"

		case "mysql":
			// Format: [username[:password]@][protocol[(address)]]/dbname[?param1=value1&...&paramN=valueN]
			config.Database.Dsn = "sdlab:sdlab@/sdlab"
		}
	}

	return err
}
