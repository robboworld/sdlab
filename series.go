package main

import (
	"errors"
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
func startSeries(values []ValueId, period time.Duration, count int) (<-chan *SerData, chan<- int, error) {
	// check that count does not exceed resolution
	for _, v := range values {
		if pluggedSensors[v.Sensor].Values[v.ValueIdx].Resolution > period {
			err := errors.New("cannot read values so quickly")
			return nil, nil, err
		}
	}
	out := make(chan *SerData, config.Series.Buffer)
	stop := make(chan int, 1)
	// starting measurements
	go func() {
		ti := time.NewTicker(period)
		readings := make([](chan float64), len(values))
		for i := range readings {
			readings[i] = make(chan float64, 1)
		}
		for t := range ti.C {
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
				break
			}
			// or series is comlete
			count--
			if count == 0 {
				close(out)
				break
			}
		}
	}()
	return out, stop, nil
}

func getSerData(s string, id int, c chan float64) {
	sr, f := pluggedSensors[s]
	if !f {
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
