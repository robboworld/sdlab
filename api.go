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
	"github.com/pborman/uuid"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"time"
	"fmt"
	"os/exec"
	"strings"
	"bytes"
)

type Lab struct {
	series map[string]*SeriesRecord
}

type SeriesRecord struct {
	data     *<-chan *SerData
	stop       *chan<- int
	finished *<-chan int
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

type APISeriesRecord struct {
	UUID     string
	Stop     bool
	Finished bool
	Len      uint
	//Created time.Time
}

type MonitorOpts struct {
	Exp_id   int
	Setup_id int
	Step     uint         // Interval
	Count    uint         // Amount
	Duration uint         // Duration / time_det
	StopAt   time.Time
	Values   []ValueId
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
	Start time.Time  `json:",omitempty"`
	End   time.Time  `json:",omitempty"`
	Step  time.Duration
}

type MonRemoveOpts struct {
	UUID     string
	WithData bool
}

type MonStrobeOpts struct {
	UUID       string
	Opts       *MonitorOpts  `json:",omitempty"`  // can omit if use UUID, else error
	OptsStrict *bool         `json:",omitempty"`  // can omit if use UUID, else false by default
}

type TimeSetOpts struct {
	TZ       string
	Datetime time.Time
	Reboot   bool
}

type CamData struct {
	Index    uint
	Device   string
	Name     string
}

type CamStreamData struct {
	Index    uint
	Device   string
	Stream   int
	Port     uint
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
	if ok, _ := valueAvailable((*valueId).Sensor, (*valueId).ValueIdx); !ok {
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

func (lab *Lab) StartSeries(opts *SeriesOpts, u *string) error {
	// Check pool size and cleanup?
	if len(lab.series) >= int(config.Series.Pool) {
		/*
		// XXX: skip cleanup now because unknown rule of series deletion (when and which of them)? just return busy
		err := lab.cleanupSeries()
		if err != nil {
			*u = ""
			return err
		}
		*/
		*u = ""
		return errors.New("series is busy")
	}

	data, stop, finished, err := startSeries(opts.Values, opts.Period, opts.Count)
	if err != nil {
		*u = ""
		return err
	}

	*u = uuid.NewRandom().String()
	lab.series[*u] = &SeriesRecord{
		data:     &data,
		stop:     &stop,
		finished: &finished,
	}

	return nil
}

func (lab *Lab) StopSeries(u *string, ok *bool) error {
	if *u == "" {
		*ok = false
		return errors.New("wrong series uuid")
	}

	s, exists := lab.series[*u]
	if !exists {
		*ok = false
		return errors.New("series is not running")
	}

	if s.stop == nil {
		*ok = false
		return errors.New("series is not running")
	}

	*s.stop <- 1
	s.stop = nil

	*ok = true
	return nil
}

func (lab *Lab) GetSeries(u *string, data *[]*SerData) error {
	if *u == "" {
		return errors.New("wrong series uuid")
	}

	s, exists := lab.series[*u]
	if !exists {
		return errors.New("series is not running")
	}

	if s.data == nil {
		return errors.New("no series ever run")
	}
	*data = make([]*SerData, len(*s.data))
	for i := range *data {
		(*data)[i] = <-*s.data
	}
	return nil
}

func (lab *Lab) ListSeries(ptr uintptr, result *[]APISeriesRecord) error {
	*result = make([]APISeriesRecord, 0)

	for k, s := range lab.series {
		st, finished := false, false
		if s.stop == nil {
			st = true
		}
		if len(*s.finished) > 0 {
			finished = true
		}
		sr := APISeriesRecord{
			k,
			st,
			finished,
			uint(len(*s.data)),
			//s.Created,
		}

		*result = append(*result, sr)
	}

	return nil
}

func (lab *Lab) RemoveSeries(u *string, ok *bool) error {
	if *u == "" {
		*ok = false
		return errors.New("wrong series uuid")
	}

	s, exists := lab.series[*u]
	if !exists {
		*ok = false
		return errors.New("series is not exists")
	}

	// stop if not stopped
	if s.stop != nil {
		*s.stop <- 1
		s.stop = nil
	}

	delete(lab.series, *u)
	//logger.Print("series " + *u + " removed")

	*ok = true
	return nil
}

func (lab *Lab) CleanSeries(ptr uintptr, ok *bool) error {
	// Warning! Will be removed ALL series!
	for k, s := range lab.series {
		// stop if not stopped
		if s.stop != nil {
			*s.stop <- 1
			s.stop = nil
		}

		delete(lab.series, k)
	}

	logger.Print("series removed")

	*ok = true
	return nil
}

func (lab *Lab) cleanupSeries() error {
	// search stopped/old series and delete one
	deleted := false
	
	for k, s := range lab.series {
		found := false
		if s.stop == nil {
			// if channel not initialized, removed or nil`ed
			found = true
		} else if len(*s.stop) > 0 {
			// already stopped
			found = true
		} else if len(*s.finished) > 0 {
			// already finished himself
			found = true
		}

		// Delete from list
		if found {
			delete(lab.series, k)
			deleted = true
			logger.Print("series " + k + " purged")
			break
		}
	}

	if !deleted {
		// TODO: try stop older series, need creation time

		//old := ""
		//if lab.series[old].stop != nil {
		//	*lab.series[old].stop <- 1
		//	lab.series[old].stop = nil
		//}

		return errors.New("series is busy")
	}

	return nil
}

func (lab *Lab) StartMonitor(opts *MonitorOpts, uuid *string) error {
	mon, err := createRunMonitor(opts)
	if err != nil {
		return err
	}

	*uuid = mon.UUID.String()

	logger.Printf("StartMonitor: started %s", *uuid)
	return nil
}

func (lab *Lab) StopMonitor(u *string, ok *bool) error {
	mon, exist := monitors[uuid.Parse(*u).String()]
	if !exist {
		*ok = false
		return errors.New("Wrong monitor UUID: " + *u)
	}

	*ok = false
	err := mon.Stop()
	if err == nil {
		*ok = true
	}

	return err
}

func (lab *Lab) ListMonitors(ptr uintptr, result *[]APIMonitor) error {
	*result = make([]APIMonitor, 0)

	// TODO: sync list monitors with monitor info

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
	mon, exist := monitors[uuid.Parse(*u).String()]
	if !exist {
		return errors.New("Wrong monitor UUID: " + *u)
	}

	// TODO: sync list monitors with monitor info

	i, err := mon.Info()
	if err != nil {
		return err
	}
	*info = *i
	return nil
}

func (lab *Lab) RemoveMonitor(opts *MonRemoveOpts, ok *bool) error {
	*ok = true
	mon, exist := monitors[uuid.Parse(opts.UUID).String()]
	if !exist {
		*ok = false
		return errors.New("Wrong monitor UUID: " + opts.UUID)
	}
	err := mon.Remove(opts.WithData)
	if err != nil {
		*ok = false
	}
	return err
}

func (lab *Lab) StrobeMonitor(opts *MonStrobeOpts, ok *bool) error {
	var monDBi *MonitorDBItem
	var err error
	strict := false

	*ok = true
	if opts.UUID != "" {
		// Monitor sensors values
		mon, exist := monitors[uuid.Parse(opts.UUID).String()]
		if !exist {
			*ok = false
			return errors.New("Wrong monitor UUID: " + opts.UUID)
		}

		monDBi, err = monitorToDB(mon)
		if err != nil {
			*ok = false
			return err
		}
	} else {
		// Custom sensors values (not use real Monitor)
		if opts.Opts == nil {
			*ok = false
			return errors.New("Empty strob parameters")
		}
		if opts.OptsStrict != nil {
			strict = *(opts.OptsStrict)
		}

		monDBi = &MonitorDBItem{
			Exp_id: opts.Opts.Exp_id,
			Values: make([]MonValue, len(opts.Opts.Values)),
		}
		// Convert Values type
		for i := range opts.Opts.Values {
			monDBi.Values[i] = MonValue{
				Sensor:   opts.Opts.Values[i].Sensor,
				ValueIdx: opts.Opts.Values[i].ValueIdx,
			}
		}
	}

	err = runStrobe(monDBi, strict)
	if err != nil {
		*ok = false
	}

	return err
}

func (lab *Lab) GetMonData(opts *MonFetchOpts, data *[]*SerData) error {
	mon, exist := monitors[uuid.Parse(opts.UUID).String()]
	if !exist {
		return errors.New("Wrong monitor UUID: " + opts.UUID)
	}

	fr, err := mon.Fetch(opts.Start, opts.End, opts.Step)
	if err != nil {
		return err
	}

	// collect plain fetched results to series rows
	*data = make([]*SerData, 0, fr.RowCnt)
	nvals := len(fr.DsNames)
	var pasttm time.Time
	var d *SerData
	added := false
	for i, _ := range fr.DsData {
		tm := fr.DsData[i].Time

		// new series data on new time
		if i == 0 || !tm.Equal(pasttm) {
			d = &SerData{
				tm,
				make([]float64, nvals),
			}
			pasttm = tm
			added = false
		}

		// copy found reading
		for j, dn := range fr.DsNames {
			if fr.DsData[i].Name == dn {
				d.Readings[j] = fr.DsData[i].Detection
			}
		}

		// add collected row
		if !added {
			*data = append(*data, d)
			added = true
		}
	}
	return nil
}

func (lab *Lab) SetDatetime(opts *TimeSetOpts, ok *bool) error {
	*ok = true

	// Set date and time (UTC)
	// Format: %m%d%H%M%Y.%S
	dt := fmt.Sprintf("%02d%02d%02d%02d%d.%02d", opts.Datetime.Month(), opts.Datetime.Day(), opts.Datetime.Hour(), opts.Datetime.Minute(), opts.Datetime.Year(), opts.Datetime.Second())
	out, err := exec.Command("date", "-u", dt).Output()
	if err != nil {
		*ok = false
		return errors.New("Set datetime failed: " + err.Error())
	}
	logger.Printf("Set datetime to %s\n", out)

	/**
	 *
	 * TODO: Save new time to RTC timer if exists
	 *
	 */

	// Set timezone
	if opts.TZ != "" {
		// XXX: TZ update is not works with "sh -c", it's shows error "sh:1:Not found..."
		/*
		cmdtz := fmt.Sprintf("'echo %s >/etc/timezone && /usr/sbin/dpkg-reconfigure -f noninteractive tzdata'", opts.TZ)
		out, err := exec.Command("sh", "-c", cmdtz).Output()
		if err != nil {
			*ok = false
			return errors.New("Set timezone failed: " + err.Error())
		}
		*/

		// Use batch script to set TZ and reconfigure
		_, err = exec.Command("changetz.sh", opts.TZ).Output()
		if err != nil {
			*ok = false
			return errors.New("Set timezone failed: " + err.Error())
		}
		logger.Printf("Set timezone to %s\n", opts.TZ)
	}

	// Reboot (need only if changed TZ)
	if opts.Reboot {
		/*
		// XXX: not works (blocks thread)
		//_, err = exec.Command("/sbin/shutdown", "-r", "-t ", "5", "now").Output()
		*/
		// Use nonblocking method - script with call shutdown scheduled as:
		//   echo "shutdown -r now" | at now + 1 minute
		// minimum delay is 1 min :(
		_, err = exec.Command("sdlabreboot.sh").Output()
		if err != nil {
			*ok = false
			return errors.New("Update timezone error, cannot reboot: " + err.Error())
		}
		logger.Println("Reboot started...")
	}

	return nil
}

func (lab *Lab) ListVideos(ptr uintptr, data *[]*CamData) error {
	// Shell script for enum devices
	out, err := exec.Command("camlist.sh").Output()
	if err != nil {
		return errors.New("List videos failed: " + err.Error())
	}

	// Parse output for video devices info
	buf := bytes.NewBuffer(out)
	lines := strings.Split(buf.String(), "\n")
	*data = make([]*CamData, 0)

	for _, s := range lines {
		if !strings.Contains(s, ":") {
			continue
		}
		cd := CamData{
			0,
			"",
			"",
		}
		attr := strings.Split(s, ":")
		lattr := len(attr)
		if lattr > 1 {
			cd.Name = attr[1]
		}
		if lattr > 0 {
			cd.Device = attr[0]
			var idx uint
			_, err = fmt.Sscanf(cd.Device, "/dev/video%d", &idx)
			if err != nil {
				continue
			}
			cd.Index = idx
		}

		*data = append(*data, &cd)
	}

	return nil
}

func (lab *Lab) GetVideoStream(device *string, info *CamStreamData) error {
	out, err := exec.Command("mjpgcmdline.sh").Output()
	if err != nil {
		return errors.New("List videos failed: " + err.Error())
	}

	// Init struct
	csd := CamStreamData{
		0,
		"",
		-1,
		8090,
	}

	var iinp int
	var idx uint
	var pnum uint

	// Parse output for video devices info
	buf := bytes.NewBuffer(out)
	lines := strings.Split(buf.String(), "\n")
	iinp = 0
	for _, s := range lines {
		if strings.Contains(s, "input_uvc.so") {
			// Input uvc plugin args
			iinp++;

			args := strings.Split(s, " ")
			dname := ""
			for _, arg := range args {
				if !strings.HasPrefix(arg, "/dev/video") {
					continue
				} else {
					dname = arg
					break
				}
			}
			// Use only given device names (not used /dev/video0 by default)
			// and skip non requested devices
			if dname == "" || dname != *device {
				continue
			}
			csd.Device = dname

			// Get device index
			_, err = fmt.Sscanf(csd.Device, "/dev/video%d", &idx)
			if err != nil {
				continue
			}
			csd.Index = idx
			csd.Stream = iinp - 1

		} else if strings.Contains(s, "output_http.so") {
			// Output http plugin args

			args := strings.Split(s, " ")
			pnum = 0
			for i, arg := range args {
				if arg != "-p" && arg != "--port" {
					continue
				} else {
					// If exists port number after prefix (parse last args string part)
					_, err = fmt.Sscanf(strings.Join(args[i:], " "), "%d", &idx)
					if err != nil {
						// Not set port number
						break
					}
					pnum = idx
					break
				}
			}
			if pnum > 0 {
				csd.Port = pnum
			}

			continue
		} else {
			continue
		}
	}

	*info = csd

	return nil
}

func (lab *Lab) StartVideoStream(device *string, ok *bool) error {
	*ok = true

	// Get currect streaming configuration
	out, err := exec.Command("mjpgcmdline.sh").Output()
	if err != nil {
		*ok = false
		return errors.New("List videos failed: " + err.Error())
	}

	devices := make([]string, 0)

	// Parse output for video devices info
	buf := bytes.NewBuffer(out)
	lines := strings.Split(buf.String(), "\n")
	exist := false
	for _, s := range lines {
		if strings.Contains(s, "input_uvc.so") {
			// Input uvc plugin args

			args := strings.Split(s, " ")
			dname := ""
			for _, arg := range args {
				if !strings.HasPrefix(arg, "/dev/video") {
					continue
				} else {
					dname = arg
					break
				}
			}
			// Match only given device names (dont check /dev/video0 as default)
			// and skip unknown devices
			if dname == "" {
				continue
			}

			// Collect enabled devices and check if exixts requested device
			devices = append(devices, dname)
			if dname == *device {
				exist = true
			}
		}
	}

	if !exist {
		devices = append(devices, *device)
	}

	if len(devices) == 0 {
		*ok = false
		return errors.New("Empty cameras list: ")
	}

	sargs := make([]string, 0)
	sargs = append(sargs, "mjpg_streamer", "start")
	sargs = append(sargs, devices...)

	// Restart service with new streaming devices
	_, err = exec.Command("service", sargs...).Output()
	if err != nil {
		*ok = false
		return errors.New("Error restart video streaming service : " + err.Error())
	}

	return nil
}

func (lab *Lab) StartVideoStreamAll(ptr uintptr, ok *bool) error {
	*ok = true

	sargs := make([]string, 0)
	sargs = append(sargs, "mjpg_streamer", "start")

	// Restart service with new streaming devices
	_, err := exec.Command("service", sargs...).Output()
	if err != nil {
		*ok = false
		return errors.New("Error start video streaming service : " + err.Error())
	}

	return nil
}

func (lab *Lab) StopVideoStream(device *string, ok *bool) error {
	*ok = true

	// Get currect streaming configuration
	out, err := exec.Command("mjpgcmdline.sh").Output()
	if err != nil {
		*ok = false
		return errors.New("List videos failed: " + err.Error())
	}

	devices := make([]string, 0)

	// Parse output for video devices info
	buf := bytes.NewBuffer(out)
	lines := strings.Split(buf.String(), "\n")
	exist := false
	for _, s := range lines {
		if strings.Contains(s, "input_uvc.so") {
			// Input uvc plugin args

			args := strings.Split(s, " ")
			dname := ""
			for _, arg := range args {
				if !strings.HasPrefix(arg, "/dev/video") {
					continue
				} else {
					dname = arg
					break
				}
			}
			// Match only given device names (dont check /dev/video0 as default)
			// and skip unknown devices
			if dname == "" {
				continue
			}

			// Collect enabled devices and skip requested to stop 
			if dname == *device {
				exist = true
			} else {
				devices = append(devices, dname)
			}
		}
	}

	// Not found device to stop streaming, already stopped?!
	if !exist {
		return nil
	}

	sargs := make([]string, 0)
	if len(devices) == 0 {
		// Just stops the service if no devices
		sargs = append(sargs, "mjpg_streamer", "stop")
	} else {
		// XXX: Must be "start" action to stop-and-start with new devices
		sargs = append(sargs, "mjpg_streamer", "start")
		sargs = append(sargs, devices...)
	}

	// Stop/Start service with new streaming devices
	_, err = exec.Command("service", sargs...).Output()
	if err != nil {
		*ok = false
		return errors.New("Error stop video streaming service : " + err.Error())
	}

	return nil
}

func (lab *Lab) StopVideoStreamAll(ptr uintptr, ok *bool) error {
	*ok = true

	sargs := make([]string, 0)
	sargs = append(sargs, "mjpg_streamer", "stop")

	// Stop service with all streaming devices
	_, err := exec.Command("service", sargs...).Output()
	if err != nil {
		*ok = false
		return errors.New("Error stop video streaming service : " + err.Error())
	}

	return nil
}

func listenUnix(path string, uid, gid int, mode os.FileMode) (listener *net.UnixListener, err error) {
	socketAddr := net.UnixAddr{Name:path, Net:"unix"}
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
	lab := &Lab{series: make(map[string]*SeriesRecord)}

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
		go func(iListener int) {
			for {
				conn, err := listeners[iListener].Accept()
				if err != nil {
					logger.Print(err)
					continue
				}
				go jsonrpc.ServeConn(conn)
			}
		}(i)
	}
	return listeners, nil
}
