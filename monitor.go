package main

import (
	"code.google.com/p/go-uuid/uuid"
	"errors"
	"fmt"
	"github.com/ziutek/rrd"
	"gopkg.in/yaml.v1"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type MonValue struct {
	Name     string
	Sensor   string
	ValueIdx int
	Type     ValueType
	previous float64
}

type RRA struct {
	CF    string
	Xff   float64
	Steps uint
	Rows  uint
}

type Monitor struct {
	Active   bool
	UUID     uuid.UUID
	Step     uint
	Values   []MonValue
	Archives []RRA
	Created  time.Time
	StopAt   time.Time
	stop     chan int
}

type MonitorYAML struct {
	Active   bool
	UUID     string
	Step     uint
	Values   []MonValue
	Archives []RRA
	Created  string
	StopAt   string "stopat,omitempty"
}

type ArchiveInfo struct {
	Step uint
	Len  uint
}

type MonitorInfo struct {
	Created  time.Time
	StopAt   time.Time
	Last     time.Time
	Archives []ArchiveInfo
}

var monitors map[string]*Monitor

func monitorToYAML(mon *Monitor) (monYAML *MonitorYAML, err error) {
	uuid := mon.UUID.String()
	monYAML = &MonitorYAML{
		mon.Active,
		uuid,
		mon.Step,
		mon.Values,
		mon.Archives,
		mon.Created.Format(time.RFC3339Nano),
		mon.StopAt.Format(time.RFC3339Nano),
	}
	return monYAML, nil
}

func monitorFromYAML(monYAML *MonitorYAML) (mon *Monitor, err error) {
	uuid := uuid.Parse(monYAML.UUID)
	created, err := time.Parse(time.RFC3339Nano, monYAML.Created)
	if err != nil {
		return nil, err
	}
	stopAt, err := time.Parse(time.RFC3339Nano, monYAML.StopAt)
	if err != nil {
		return nil, err
	}
	mon = &Monitor{
		monYAML.Active,
		uuid,
		monYAML.Step,
		monYAML.Values,
		monYAML.Archives,
		created,
		stopAt,
		nil,
	}
	return mon, nil
}

// loadMonitor reads YAML file by specified path and creates a new Monitor
// object.
func loadMonitor(path string) (*Monitor, error) {
	yml, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var monYAML MonitorYAML
	err = yaml.Unmarshal(yml, &monYAML)
	if err != nil {
		return nil, err
	}
	mon, err := monitorFromYAML(&monYAML)
	if err == nil && mon.Active {
		err = mon.Run()
	}
	return mon, err
}

// loadRunMonitors looks for saved monitors, loads them and run those having
// state active.
func loadRunMonitors() error {
	files, err := filepath.Glob(config.Monitor.Path + "/*.yml")
	if err != nil {
		return err
	}
	monitors = make(map[string]*Monitor, len(files))
	for _, f := range files {
		mon, err := loadMonitor(f)
		if err != nil {
			logger.Print(err)
			continue
		}
		if mon.Active {
			if mon.StopAt.IsZero() || mon.StopAt.After(time.Now()) {
				err = mon.Run()
			} else {
				mon.Active = false
				err = mon.save()
			}
			if err != nil {
				logger.Print(err)
			}
		}
		monitors[string(mon.UUID)] = mon
	}
	return nil
}

func (mon *Monitor) RRDPath() string {
	path := config.Monitor.Path + "/" + mon.UUID.String() + ".rrd"
	return path
}

func (mon *Monitor) YMLPath() string {
	path := config.Monitor.Path + "/" + mon.UUID.String() + ".yml"
	return path
}

func (mon *Monitor) CreateRRD() error {
	path := mon.RRDPath()
	c := rrd.NewCreator(path, time.Now(), mon.Step)
	for i, v := range mon.Values {
		if !valueAvailable(v.Sensor, v.ValueIdx) {
			return errors.New("Wrong sensor/value spec")
		}
		name := pluggedSensors[v.Sensor].Values[v.ValueIdx].Name + strconv.Itoa(i)
		c.DS(name,
			v.Type.String(),
			2*mon.Step,
			pluggedSensors[v.Sensor].Values[v.ValueIdx].Range.Min,
			pluggedSensors[v.Sensor].Values[v.ValueIdx].Range.Max,
		)
		mon.Values[i].Name = name
	}
	for _, rra := range mon.Archives {
		c.RRA(rra.CF, rra.Xff, rra.Steps, rra.Rows)
	}
	err := c.Create(true)
	if err != nil {
		return err
	}
	return mon.save()
}

func (mon *Monitor) Run() error {
	d := time.Duration(mon.Step) * time.Second
	t := time.NewTicker(d)
	readings := make([](chan float64), len(mon.Values))
	for i := range readings {
		readings[i] = make(chan float64, 1)
	}
	mon.stop = make(chan int, 1)
	path := mon.RRDPath()
	u := rrd.NewUpdater(path)
	vals := make([]interface{}, len(mon.Values)+1)
	go func() {
		for tm := range t.C {
			if (!mon.StopAt.IsZero()) && mon.StopAt.Before(tm) {
				mon.Stop()
			}
			if len(mon.stop) > 0 {
				return
			}
			for i, v := range mon.Values {
				go getSerData(v.Sensor, v.ValueIdx, readings[i])
			}
			vals[0] = tm
			for i, c := range readings {
				vals[i+1] = <-c
				mon.Values[i].previous = vals[i+1].(float64)
			}
			u.Update(vals...)
		}
	}()
	return nil
}

func (mon *Monitor) Stop() error {
	if !mon.Active {
		return errors.New("Monitor " + mon.UUID.String() + " is inactive")
	}
	mon.stop <- 1
	mon.Active = false
	return mon.save()
}

func (mon *Monitor) save() error {
	monYAML, err := monitorToYAML(mon)
	if err != nil {
		return err
	}
	yml, err := yaml.Marshal(monYAML)
	if err != nil {
		return err
	}
	fn := mon.YMLPath()
	err = ioutil.WriteFile(fn, yml, 0644)
	return err
}

func (mon *Monitor) Info() (*MonitorInfo, error) {
	ri, err := rrd.Info(mon.RRDPath())
	if err != nil {
		return nil, err
	}
	n := len(ri["rra.xff"].([]interface{})) // number of archives
	ai := make([]ArchiveInfo, n)
	for i := range ai {
		ai[i] = ArchiveInfo{
			ri["rra.pdp_per_row"].([]interface{})[i].(uint) * ri["step"].(uint), // archive data step
			ri["rra.rows"].([]interface{})[i].(uint),
		}
	}
	mi := &MonitorInfo{
		mon.Created,
		mon.StopAt,
		time.Unix(int64(ri["last_update"].(uint)), 0),
		ai,
	}
	return mi, nil
}

func (mon *Monitor) Fetch(start, end time.Time, step time.Duration) (rrd.FetchResult, error) {
	fr, err := rrd.Fetch(mon.RRDPath(), "AVERAGE", start, end, step)
	return fr, err
}

func (mon *Monitor) Remove() error {
	if mon.Active {
		err := mon.Stop()
		if err != nil {
			logger.Print("error stopping monitor being removed: " + err.Error())
		}
	}
	delete(monitors, string(mon.UUID))
	err := os.Remove(mon.RRDPath())
	if err != nil {
		logger.Print("error removing monitor data: " + err.Error())
	}
	err = os.Remove(mon.YMLPath())
	if err != nil {
		err = errors.New("error removing monitor configuration: " + err.Error())
		logger.Print(err)
	}
	return err
}

func newMonitor(opts *MonitorOpts) (*Monitor, error) {
	if (!opts.StopAt.IsZero()) && opts.StopAt.Before(time.Now()) {
		err := errors.New("monitor stop time is in the past")
		return nil, err
	}
	vals := make([]MonValue, len(opts.Values))
	for i, v := range opts.Values {
		if pluggedSensors[v.Sensor] == nil {
			err := errors.New("no sensor `" + v.Sensor + "' connected")
			return nil, err
		}
		if len(pluggedSensors[v.Sensor].Values) <= v.ValueIdx {
			err := fmt.Errorf("no value %d for sensor `%s' available",
				v.ValueIdx, v.Sensor)
			return nil, err
		}
		vals[i] = MonValue{
			"",
			v.Sensor,
			v.ValueIdx,
			pluggedSensors[v.Sensor].Values[v.ValueIdx].Type,
			0,
		}
	}
	rras := make([]RRA, 3)
	rras[0] = RRA{"AVERAGE", 0.5, 1, opts.Count}
	rras[1] = RRA{"AVERAGE", 0.5, 4, opts.Count}
	rras[2] = RRA{"AVERAGE", 0.5, 16, opts.Count}
	mon := Monitor{
		true,
		uuid.NewRandom(),
		opts.Step,
		vals,
		rras,
		time.Now(),
		opts.StopAt,
		nil,
	}
	return &mon, nil
}

func createRunMonitor(opts *MonitorOpts) (*Monitor, error) {
	mon, err := newMonitor(opts)
	if err != nil {
		return mon, err
	}
	err = mon.CreateRRD()
	if err != nil {
		return mon, err
	}
	err = mon.Run()
	if err != nil {
		return mon, err
	}
	mon.Created = time.Now()
	monitors[string(mon.UUID)] = mon
	return mon, nil
}
