package main

import (
	"code.google.com/p/go-uuid/uuid"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"time"
)

type Lab struct {
	series *<-chan *SerData
	stop   *chan<- int
}

type ValueId struct {
	Sensor   string
	ValueIdx int
}

type APISensor struct {
	Values []APIValue
}

type APIValue struct {
	Name       string
	Range      DataRange
	Resolution time.Duration
}

type APISensors map[string]APISensor

type Data struct {
	Time    time.Time
	Reading float64
}

type SeriesOpts struct {
	Values []ValueId
	Period time.Duration
	Count  int
}

type MonitorOpts struct {
	Values []ValueId
	Step   uint
	Count  uint
	StopAt time.Time
}

type APIMonValue struct {
	Name string
	ValueId
}

type APIMonitor struct {
	Active  bool
	UUID    string
	Created time.Time
	StopAt  time.Time
	Values  []APIMonValue
}

type MonFetchOpts struct {
	UUID  string
	Start time.Time
	End   time.Time
	Step  time.Duration
}

// Implement json.Marshaler interface to handle not-a-number values.
func (sd SerData) MarshalJSON() ([]byte, error) {
	r := "["
	var err error
	n := len(sd.Readings)
	for i, d := range sd.Readings {
		if math.IsNaN(d) {
			r += "\"NaN\""
		} else if math.IsInf(d, +1) {
			r += "\"+Inf\""
		} else if math.IsInf(d, -1) {
			r += "\"-Inf\""
		} else {
			rb, err := json.Marshal(d)
			if err != nil {
				return []byte("{}"), err
			}
			r += string(rb)
		}
		if i < n-1 {
			r += ","
		}
	}
	r += "]"
	t, err := json.Marshal(sd.Time)
	if err != nil {
		return []byte("{}"), err
	}
	j := "{\"Time\":" + string(t) + ",\"Readings\":" + r + "}"
	return []byte(j), nil
}

func (lab *Lab) GetData(valueId *ValueId, value *Data) (err error) {
	(*value).Time = time.Now()
	if !valueAvailable((*valueId).Sensor, (*valueId).ValueIdx) {
		return errors.New("Wrong sensor spec")
	}
	(*value).Reading, err = pluggedSensors[(*valueId).Sensor].GetData((*valueId).ValueIdx)
	return err
}

func (lab *Lab) ListSensors(rescan *bool, sensors *APISensors) error {
	*sensors = make(APISensors, len(pluggedSensors))
	if *rescan {
		err := scanSensors()
		if err != nil {
			logger.Print(err)
			return err
		}
	}
	for id, sen := range pluggedSensors {
		var sensor APISensor
		for _, val := range sen.Values {
			sensor.Values = append(sensor.Values,
				APIValue{
					val.Name,
					val.Range,
					val.Resolution,
				},
			)
		}
		(*sensors)[id] = sensor
	}
	return nil
}

func (lab *Lab) StartSeries(opts *SeriesOpts, ok *bool) error {
	if lab.stop != nil {
		*lab.stop <- 1
		lab.stop = nil
	}
	data, stop, err := startSeries(opts.Values, opts.Period, opts.Count)
	if err != nil {
		*ok = false
		return err
	}
	lab.series = &data
	lab.stop = &stop
	*ok = true
	return nil
}

func (lab *Lab) StopSeries(ptr uintptr, ok *bool) error {
	if lab.stop == nil {
		*ok = false
		return errors.New("no series running")
	}
	*lab.stop <- 1
	lab.stop = nil
	*ok = true
	return nil
}

func (lab *Lab) GetSeries(ptr uintptr, data *[]*SerData) error {
	if lab.series == nil {
		return errors.New("no series ever run")
	}
	*data = make([]*SerData, len(*lab.series))
	for i := range *data {
		(*data)[i] = <-*lab.series
	}
	return nil
}

func (lab *Lab) StartMonitor(opts *MonitorOpts, uuid *string) error {
	mon, err := createRunMonitor(opts)
	if err != nil {
		return err
	}
	monitors[string(mon.UUID)] = mon
	*uuid = mon.UUID.String()
	return nil
}

func (lab *Lab) StopMonitor(u *string, ok *bool) error {
	mon, exist := monitors[string(uuid.Parse(*u))]
	if !exist {
		*ok = false
		return errors.New("Wrong monitor UUID: " + *u)
	}
	err := mon.Stop()
	if err == nil {
		*ok = true
	} else {
		*ok = false
	}
	return err
}

func (lab *Lab) ListMonitors(ptr uintptr, result *[]APIMonitor) error {
	*result = make([]APIMonitor, 0)
	for _, v := range monitors {
		m := APIMonitor{
			v.Active,
			v.UUID.String(),
			v.Created,
			v.StopAt,
			make([]APIMonValue, len(v.Values)),
		}
		for i, vl := range v.Values {
			m.Values[i] = APIMonValue{
				vl.Name,
				ValueId{
					vl.Sensor,
					vl.ValueIdx,
				},
			}
		}
		*result = append(*result, m)
	}
	return nil
}

func (lab *Lab) GetMonInfo(u *string, info *MonitorInfo) error {
	mon, exist := monitors[string(uuid.Parse(*u))]
	if !exist {
		return errors.New("Wrong monitor UUID: " + *u)
	}
	i, err := mon.Info()
	if err != nil {
		return err
	}
	*info = *i
	return nil
}

func (lab *Lab) RemoveMonitor(u *string, ok *bool) error {
	*ok = true
	mon, exist := monitors[string(uuid.Parse(*u))]
	if !exist {
		*ok = false
		return errors.New("Wrong monitor UUID: " + *u)
	}
	err := mon.Remove()
	if err != nil {
		*ok = false
	}
	return err
}

func (lab *Lab) GetMonData(opts *MonFetchOpts, data *[]*SerData) error {
	mon, exist := monitors[string(uuid.Parse(opts.UUID))]
	if !exist {
		return errors.New("Wrong monitor UUID: " + opts.UUID)
	}
	fr, err := mon.Fetch(opts.Start, opts.End, opts.Step)
	if err != nil {
		return err
	}
	defer fr.FreeValues()
	*data = make([]*SerData, 0, fr.RowCnt)
	nvals := len(fr.DsNames)
	row := 0
	for t := fr.Start; t.Before(fr.End) || t.Equal(fr.End); t = t.Add(fr.Step) {
		d := SerData{
			t,
			make([]float64, nvals),
		}
		for ds := range d.Readings {
			d.Readings[ds] = fr.ValueAt(ds, row)
		}
		*data = append(*data, &d)
		row++
	}
	return nil
}

func listenUnix(path string, uid, gid int, mode os.FileMode) (listener *net.UnixListener, err error) {
	socketAddr := net.UnixAddr{path, "unix"}
	listener, err = net.ListenUnix(socketAddr.Network(), &socketAddr)
	if err != nil {
		return nil, err
	}
	err = os.Chmod(path, mode)
	if err != nil {
		logger.Print(err)
	}
	err = os.Chown(path, uid, gid)
	if err != nil {
		logger.Print(err)
	}
	return listener, nil
}

func listenTCP(addr string) (listener *net.TCPListener, err error) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, err
	}
	listener, err = net.ListenTCP(tcpAddr.Network(), tcpAddr)
	if err != nil {
		return nil, err
	}
	return listener, nil
}

func startAPI() (listeners []net.Listener, err error) {
	lab := new(Lab)
	rpc.Register(lab)
	listeners = make([]net.Listener, 0, 2)
	if config.Socket.Enable {
		l, err := listenUnix(
			config.Socket.Path,
			int(*config.Socket.User),
			int(*config.Socket.Group),
			config.Socket.Mode,
		)
		if err != nil {
			logger.Print(err)
		} else {
			listeners = append(listeners, l)
			logger.Print("Started listening ", l.Addr())
		}
	}
	if config.TCP.Enable {
		l, err := listenTCP(config.TCP.Listen)
		if err != nil {
			logger.Print(err)
		} else {
			listeners = append(listeners, l)
			logger.Print("Started listening ", l.Addr())
		}
	}
	for i := range listeners {
		go func() {
			for {
				conn, err := listeners[i].Accept()
				if err != nil {
					logger.Print(err)
					continue
				}
				go jsonrpc.ServeConn(conn)
			}
		}()
	}
	return listeners, nil
}
