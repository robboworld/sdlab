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
	"math"
	"time"
)

type SerData struct {
	Time     time.Time
	Readings []float64
}

// startSeries begins the series of measurements of values one time per period,
// maximum number of measurements is count.
// It returns channel to read data from, channel receiving value to stop series
// and error if any.
func startSeries(values []ValueId, period time.Duration, count int) (<-chan *SerData, chan<- int, <-chan int, error) {
	// check arguments
	if len(values) == 0 {
		return nil, nil, nil, errors.New("no sensors selected")
	}
	if period == 0 {
		return nil, nil, nil, errors.New("period must be greater than zero")
	}
	if count <= 0 {
		return nil, nil, nil, errors.New("count must be greater than zero")
	}

	// check that values are available and period does not exceed resolution
	for _, v := range values {
		if pluggedSensors[v.Sensor] == nil {
			err := errors.New("no sensor '" + v.Sensor + "' connected")
			return nil, nil, nil, err
		}
		if len(pluggedSensors[v.Sensor].Values) <= v.ValueIdx {
			err := fmt.Errorf("no value %d for sensor '%s' available",
				v.ValueIdx, v.Sensor)
			return nil, nil, nil, err
		}
		if pluggedSensors[v.Sensor].Values[v.ValueIdx].Resolution > period {
			err := errors.New("cannot read values so quickly")
			return nil, nil, nil, err
		}
	}
	out := make(chan *SerData, config.Series.Buffer)
	stop := make(chan int, 1)
	finished := make(chan int, 1)
	// starting measurements
	go func() {
		ti := time.NewTicker(period)
		readings := make([](chan float64), len(values))
		for i := range readings {
			readings[i] = make(chan float64, 1)
		}
		for {
			select {
			case t := <-ti.C:
				for i, v := range values {
					// sensors are polled simultaneously
					// to avoid lags
					go getSerData(v.Sensor, v.ValueIdx, readings[i])
				}
				data := SerData{t, make([]float64, len(values))}
				for i, c := range readings {
					data.Readings[i] = <-c
				}
				if len(out) == int(config.Series.Buffer) {
					// channel shouldn't be blocked
					// so we simply drop the oldest dataset
					<-out
				}
				out <- &data
				// check if we are enforced to stop
				if len(stop) > 0 {
					<-stop
					close(out)
					close(finished)
					return
				}
				// or series is complete
				count--
				if count == 0 {
					// stop himself
					finished <- 1
					close(finished)
					close(out)
					return
				}
			case <-stop:
				close(out)
				close(finished)
				return
			}
		}
	}()
	return out, stop, finished, nil
}

func getSerData(s string, id int, c chan float64) {
	sr, f := pluggedSensors[s]
	if !f {
		c <- math.NaN()
		return
	}
	if len(sr.Values) <= id {
		c <- math.NaN()
		return
	}
	d, err := sr.GetData(id)
	if err != nil {
		logger.Print(err)
		c <- math.NaN()
		return
	}
	c <- d
}
