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
	"github.com/pborman/uuid"
	"errors"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"database/sql"
	"strconv"
	"time"
	"strings"
	"math"
)

const (
	RFC3339_UTC      = "2006-01-02T15:04:05Z"
	RFC3339Nano_UTC  = "2006-01-02T15:04:05.999999999Z"
	RFC3339Milli     = "2006-01-02T15:04:05.999Z07:00"
	RFC3339Milli_UTC = "2006-01-02T15:04:05.999Z"
)

type MonValue struct {
	Name     string
	Sensor   string
	ValueIdx int
	Type     ValueType    // TODO: remove Type using

	previous float64
}

type MonCounters struct {
	Done     uint
	Err      uint
}

type Monitor struct {
	Id       int
	UUID     uuid.UUID
	Exp_id   int
	Setup_id int
	Step     uint         // Interval
	Amount   uint         // Amount total, 0 if StopAt mode
	Duration uint         // Duration, in StopAt mode, only passed to database
	Created  time.Time
	StopAt   time.Time
	Active   bool

	stop     chan int

	Values   []MonValue

	Counters MonCounters  // Counters
}

type MonitorDBItem struct {
	Id       int
	UUID     string
	Exp_id   int
	Setup_id int
	Step     uint
	Amount   uint
	Duration uint
	Created  string
	StopAt   string
	Active   bool

	Values   []MonValue

	Counters MonCounters
}

type DetectionItem struct {
	Id            int
	Exp_id        int
	Mon_id        int
	Time          time.Time
	Sensor_id     string
	Sensor_val_id int
	Detection     float64
	Error         string    // TODO: remode old error field
}

type DetectionDBItem struct {
	Id            int
	Exp_id        int
	Mon_id        int
	Time          string
	Sensor_id     string
	Sensor_val_id int
	Detection     float64
	Error         string    // TODO: remode old error field
}

type MonValueInfo struct {
	Name     string
	Sensor   string
	ValueIdx int
	Len      uint
}

type ArchiveInfo struct {
	Step uint
	Len  uint
}

type MonitorInfo struct {
	Active   bool
	Created  time.Time
	StopAt   time.Time
	Last     time.Time
	Amount   uint
	Duration uint
	Counters MonCounters
	Archives []ArchiveInfo
	Values   []MonValueInfo
}

type FetchResultDBItem struct {
	Time          time.Time
	Name          string
	Detection     float64
	Error         string    // TODO: remode old error field
}

type FetchResultDB struct {
	Filename string
	Cf       string
	Start    time.Time
	End      time.Time
	Step     time.Duration
	DsNames  []string
	RowCnt   int
	DsData   []*FetchResultDBItem
	// contains filtered or unexported fields
}

var (
	db       *sql.DB
	queries  map[string]string
	stmts    map[string]*sql.Stmt
	monitors map[string]*Monitor
)

func initQueries(dbtype string) error {
	var err error

	// Prepare plain queries
	if queries == nil {
		queries = make(map[string]string)
	}

	// Database specific queries
	// - pre: prerequisite configuration, database fixes and etc.
	switch dbtype {
	case "sqlite":
		queries["_pre"] = `
			PRAGMA automatic_index = ON;
			PRAGMA busy_timeout = 50000000;
			PRAGMA cache_size = 32768;
			PRAGMA cache_spill = OFF;
			PRAGMA foreign_keys = OFF;
			PRAGMA journal_mode = WAL;
			PRAGMA journal_size_limit = 67110000;
			PRAGMA locking_mode = NORMAL;
			PRAGMA page_size = 4096;
			PRAGMA recursive_triggers = ON;
			PRAGMA secure_delete = ON;
			PRAGMA synchronous = NORMAL;
			PRAGMA temp_store = MEMORY;
			PRAGMA wal_autocheckpoint = 16384;
		`
		/*
		Sqlite Database PRAGMAs
		@see http://www.sqlite.org/pragma.html
			automatic_index    - ? (default enabled)
			busy_timeout       - sleeps for a specified amount of time when a table is locked (milliseconds, default 0)
			cache_size         - maximum number of database disk pages that SQLite will hold in memory at once 
								 per open database file. default "-2000" (cache size is limited to 2048000 bytes)
			cache_spill        - enables or disables the ability of the pager to spill dirty cache pages to 
								 the database file in the middle of a transaction (default enabled).
			foreign_keys       - default OFF
			journal_mode       - (WAL journaling mode uses a write-ahead log instead of a rollback journal 
								 to implement transactions)
			journal_size_limit - limit the size of rollback-journal and WAL files left in the file-system 
								 after transactions or checkpoints, (in MiB)
								 (the write-ahead log file is not truncated following a checkpoint)
			locking_mode       - the database connection locking-mode.
								 The locking-mode is either NORMAL or EXCLUSIVE (default NORMAL).
								 NORMAL - a database connection unlocks the database file at the conclusion of 
								 each read or write transaction
			page_size          - (default 4096)
			recursive_triggers - (default 0)
			secure_delete      - When secure-delete on, SQLite overwrites deleted content with zeros. (default 1)
			synchronous        - NORMAL - the SQLite database engine will still sync at the most critical moments, 
								 but less often than in FULL mode (default FULL)
			temp_store         - When temp_store is MEMORY temporary tables and indices are kept in as 
								 if they were pure in-memory databases memory. (default DEFAULT)
			wal_autocheckpoint - This pragma queries or sets the write-ahead log auto-checkpoint interval. 
								 (default enabled 1000)
		*/
	case "mysql":
			// TODO: mysql pragma?
			fallthrough
	default: 
		queries["_pre"] = ``
	}

	// TABLE: monitors
	queries["monitors_select_all"] = `
		SELECT *
		FROM monitors
		ORDER BY id;
	`
	queries["monitors_select_all_id"] = `
		SELECT id
		FROM monitors
		ORDER BY id;
	`
	queries["monitors_select_by_id"] = `
		SELECT *
		FROM monitors
		WHERE id = ?;
	`
	queries["monitors_count"] = `
		SELECT COUNT(*)
		FROM monitors;
	`
	queries["monitors_insert"] = `
		INSERT INTO monitors (uuid, exp_id, setup_id, interval, amount, duration, created, stopat, active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
	`
	queries["monitors_replace"] = `
		INSERT OR REPLACE INTO monitors (id, uuid, exp_id, setup_id, interval, amount, duration, created, stopat, active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`
	queries["monitors_delete_by_id"] = `
		DELETE FROM monitors
		WHERE id = ?;
	`

	// TABLE: monitors_values
	queries["monitors_values_select_by_uuid"] = `
		SELECT *
		FROM monitors_values
		WHERE uuid = ?;
	`
	queries["_monitors_values_replace_into"] = `INSERT OR REPLACE INTO monitors_values(uuid, name, sensor, valueidx)`
	queries["_monitors_values_replace_values"] = `(?, ?, ?, ?)`
	queries["monitors_values_delete_by_uuid"] = `
		DELETE FROM monitors_values
		WHERE uuid = ?;
	`

	// TABLE: monitors_counters
	queries["monitors_counters_select_by_uuid"] = `
		SELECT *
		FROM monitors_counters
		WHERE uuid = ?;
	`
	queries["monitors_counters_replace"] = `
		INSERT OR REPLACE INTO monitors_counters (uuid, done, err)
		VALUES (?, ?, ?);
	`
	queries["monitors_counters_update_all_by_uuid"] = `
		UPDATE monitors_counters
		SET done = done + ?, err = err + ?
		WHERE uuid = ?;
	`
	queries["monitors_counters_update_done_by_uuid"] = `
		UPDATE monitors_counters
		SET done = done + ?
		WHERE uuid = ?;
	`
	queries["monitors_counters_update_err_by_uuid"] = `
		UPDATE monitors_counters
		SET err = err + ?
		WHERE uuid = ?;
	`
	queries["monitors_counters_delete_by_uuid"] = `
		DELETE FROM monitors_counters
		WHERE uuid = ?;
	`

	// TABLE: detections
	queries["detections_select_by_monitor"] = `
		SELECT time, sensor_id, sensor_val_id, detection, error
		FROM detections
		WHERE (mon_id = ?)
		ORDER BY strftime("%Y-%m-%d %H:%M:%f", time), sensor_id, sensor_val_id;
	`
	queries["detections_select_by_monitor_time_from"] = `
		SELECT time, sensor_id, sensor_val_id, detection, error
		FROM detections
		WHERE (mon_id = ?) AND (strftime("%Y-%m-%d %H:%M:%f", time) >= strftime("%Y-%m-%d %H:%M:%f", ?))
		ORDER BY strftime("%Y-%m-%d %H:%M:%f", time), sensor_id, sensor_val_id;
	`
	queries["detections_select_by_monitor_time_to"] = `
		SELECT time, sensor_id, sensor_val_id, detection, error
		FROM detections
		WHERE (mon_id = ?) AND (strftime("%Y-%m-%d %H:%M:%f", time) <= strftime("%Y-%m-%d %H:%M:%f", ?))
		ORDER BY strftime("%Y-%m-%d %H:%M:%f", time), sensor_id, sensor_val_id;
	`
	queries["detections_select_by_monitor_time_range"] = `
		SELECT time, sensor_id, sensor_val_id, detection, error
		FROM detections
		WHERE (mon_id = ?) AND (strftime("%Y-%m-%d %H:%M:%f", time) BETWEEN strftime("%Y-%m-%d %H:%M:%f", ?) AND strftime("%Y-%m-%d %H:%M:%f", ?))
		ORDER BY strftime("%Y-%m-%d %H:%M:%f", time), sensor_id, sensor_val_id;
	`
	queries["detections_count_by_monitor"] = `
		SELECT COUNT(*)
		FROM detections
		WHERE mon_id = ?;
	`
	queries["detections_count_by_monitor_grouptime"] = `
		SELECT COUNT(*)
		FROM (
			SELECT time
			FROM detections
			WHERE mon_id = ?
			GROUP BY time
		);
	`
	queries["detections_count_by_monitor_sensor"] = `
		SELECT COUNT(*)
		FROM detections
		WHERE mon_id = ? AND sensor_id = ? AND sensor_val_id = ?;
	`
	queries["detections_select_last_time_by_monitor"] = `
		SELECT time
		FROM detections
		WHERE mon_id = ?
		ORDER BY strftime("%Y-%m-%d %H:%M:%f", time) DESC
		LIMIT 1;
	`
	queries["detections_insert"] = `
		INSERT INTO detections(exp_id, mon_id, time, sensor_id, sensor_val_id, detection, error)
		VALUES (?, ?, ?, ?, ?, ?, ?);
	`
	queries["_detections_insert_into"] = `INSERT INTO detections(exp_id, mon_id, time, sensor_id, sensor_val_id, detection, error)`
	queries["_detections_insert_values"] = `(?, ?, ?, ?, ?, ?, ?)`
	queries["detections_delete_by_monitor"] = `
		DELETE FROM detections
		WHERE mon_id = ?;
	`

	// Prepare statements
	stmts = make(map[string]*sql.Stmt)

	for qname, value := range queries {
		if string([]rune(qname)[0]) == "_" {
			continue
		}
		stmts[qname], err = db.Prepare(value)
		if err != nil {
			return err
		}
	}

	return nil
}

func cleanupQueries() {
	for _, stmt := range stmts {
		if stmt != nil {
			stmt.Close()
		}
	}
	// TODO: return error on Close
}

func prepareDB() error {
	if pre, ok := queries["_pre"]; !ok || (pre == "") {
		return nil
	}

	_, err := db.Exec(queries["_pre"])
	if err != nil {
		return err
	}

	return nil
}

func monitorToDB(mon *Monitor) (monDBi *MonitorDBItem, err error) {
	uuid := mon.UUID.String()
	values := make([]MonValue, len(mon.Values))
	copy(values, mon.Values)
	monDBi = &MonitorDBItem{
		mon.Id,
		uuid,
		mon.Exp_id,
		mon.Setup_id,
		mon.Step,
		mon.Amount,
		mon.Duration,
		mon.Created.UTC().Format(time.RFC3339Nano),
		mon.StopAt.UTC().Format(time.RFC3339Nano),
		mon.Active,

		values,
		mon.Counters,
	}
	return monDBi, nil
}

func monitorFromDB(mondbi *MonitorDBItem) (mon *Monitor, err error) {
	uuid := uuid.Parse(mondbi.UUID)
	created, err := time.Parse(time.RFC3339Nano, mondbi.Created)
	if err != nil {
		return nil, err
	}
	stopAt, err := time.Parse(time.RFC3339Nano, mondbi.StopAt)
	if err != nil {
		return nil, err
	}
	/*
	exp_id := 0
	if len(string(mondbi.Exp_id)) > 0 {
		exp_id, _ = strconv.Atoi(mondbi.Exp_id)
	}
	setup_id := 0
	if len(string(mondbi.Setup_id)) > 0 {
		setup_id, _ = strconv.Atoi(mondbi.Setup_id)
	}
	step := 0
	if len(string(mondbi.Step)) > 0 {
		step, _ = strconv.Atoi(mondbi.Step)
	}
	amount := 0
	if len(string(mondbi.Amount)) > 0 {
		amount, _ = strconv.Atoi(mondbi.Amount)
	}
	duration := 0
	if len(string(mondbi.Duration)) > 0 {
		duration, _ = strconv.Atoi(mondbi.Duration)
	}
	*/
	values := make([]MonValue, len(mondbi.Values))
	copy(values, mondbi.Values)
	mon = &Monitor{
		mondbi.Id,
		uuid,
		mondbi.Exp_id,
		mondbi.Setup_id,
		mondbi.Step,
		mondbi.Amount,
		mondbi.Duration,
		created,
		stopAt,
		mondbi.Active,

		nil,
		values,
		mondbi.Counters,
	}
	return mon, nil
}

// loadMonitor reads database item and creates a new Monitor
// object.
func loadMonitor(monid int) (*Monitor, error) {
	var err, err2 error

	mondbi := MonitorDBItem{}
	mondbi.Values = make([]MonValue, 0)

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}

	// Load Monitor
	row := tx.Stmt(stmts["monitors_select_by_id"]).QueryRow(monid)
	err = row.Scan(
		&mondbi.Id,
		&mondbi.UUID,
		&mondbi.Exp_id,
		&mondbi.Setup_id,
		&mondbi.Step,
		&mondbi.Amount,
		&mondbi.Duration,
		&mondbi.Created,
		&mondbi.StopAt,
		&mondbi.Active,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			// there were no rows, but otherwise no error occurred
			err = errors.New("Fatal Monitor Select Stmt QueryRow: " + err.Error())
			return nil, err
		} else {
			//fmt.Printf(LPURPLE+"loadMonitor#%05d:"+RED+" Fatal Monitor Select Stmt QueryRow %s, continue\n"+NCO, monid, err)
			logger.Print("Fatal Monitor Select Stmt QueryRow: " + err.Error())
			err2 = tx.Rollback()
			if err2 != nil {
				//fmt.Printf(LPURPLE+"loadMonitor#%05d:"+RED+" Fatal Monitor Select Stmt Rollback %s, continue\n"+NCO, monid, err2)
				logger.Print("Fatal Monitor Select Stmt Rollback: " + err2.Error())
				return nil, err2
			}
			return nil, err
		}
	}

	// Load Monitor Values
	rows, err := tx.Stmt(stmts["monitors_values_select_by_uuid"]).Query(mondbi.UUID)
	if err != nil {
		//fmt.Printf(LPURPLE+"loadMonitor#%05d:"+RED+" Fatal Monitor UUID %s Values Stmt Query %s, continue\n"+NCO, monid, mondbi.UUID, err)
		logger.Printf("Fatal Monitor UUID %s Values Stmt Query: %s\n", mondbi.UUID, err.Error())
		err2 = tx.Rollback()
		if err2 != nil {
			//fmt.Printf(LPURPLE+"loadMonitor#%05d:"+RED+" Fatal Monitor UUID %s Values Stmt Rollback %s, continue\n"+NCO, monid, mondbi.UUID, err2)
			logger.Printf("Fatal Monitor UUID %s Values Stmt Rollback: %s\n", mondbi.UUID, err2.Error())
			return nil, err2
		}
		return nil, err2
	}
	defer rows.Close()
	monuuid := ""
	for rows.Next() {
		monv := new(MonValue)

		err = rows.Scan(&monuuid, &monv.Name, &monv.Sensor, &monv.ValueIdx)
		if err != nil {
			//fmt.Printf(LPURPLE+"loadMonitor#%05d:"+RED+" Fatal Scan Monitor UUID %s Values %s, continue\n"+NCO, monid, mondbi.UUID, err)
			logger.Printf("Fatal Scan Monitor UUID %s Values: %s", mondbi.UUID, err.Error())
			// no need Rollback
			continue
		}

		mondbi.Values = append(mondbi.Values, *monv)
	}

	// Load Monitor Counters
	row = tx.Stmt(stmts["monitors_counters_select_by_uuid"]).QueryRow(mondbi.UUID)
	err = row.Scan(&monuuid, &mondbi.Counters.Done, &mondbi.Counters.Err)
	if err != nil {
		if err == sql.ErrNoRows {
			// there were no rows, but otherwise no error occurred
			err = errors.New("Fatal Monitor Counters Select Stmt QueryRow: " + err.Error())
			return nil, err
		} else {
			//fmt.Printf(LPURPLE+"loadMonitor#%05d:"+RED+" Fatal Monitor Counters Select Stmt QueryRow %s, continue\n"+NCO, monid, err)
			logger.Print("Fatal Monitor Counters Select Stmt QueryRow: " + err.Error())
			err2 = tx.Rollback()
			if err2 != nil {
				//fmt.Printf(LPURPLE+"loadMonitor#%05d:"+RED+" Fatal Monitor Counters Select Stmt Rollback %s, continue\n"+NCO, monid, err2)
				logger.Print("Fatal Monitor Counters Select Stmt Rollback: " + err2.Error())
				return nil, err2
			}
			return nil, err
		}
	}

	err = tx.Commit()
	if err != nil {
		//fmt.Printf(LPURPLE+"loadMonitor#%05d:"+RED+" Fatal Commit Load Monitor %s, exiting\n"+NCO, err)
		logger.Printf("Fatal Commit Load Monitor %s, exiting\n", err.Error())
		return nil, err
	}

	// Convert Monitor from DB 
	mon, err := monitorFromDB(&mondbi)
	if err != nil {
		return nil, err
	}

	return mon, err
}

// loadRunMonitors looks for saved monitors, loads them and run those having
// state active.
func loadRunMonitors() error {
	var err, err2 error
	var monid int

	logger.Print("Loading monitors...")

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	// Count monitors
	row := tx.Stmt(stmts["monitors_count"]).QueryRow()
	var count int64 = 0
	err = row.Scan(&count)
	if err != nil {
		if err == sql.ErrNoRows {
			// there were no rows, but otherwise no error occurred
			count = 0
		} else {
			//fmt.Printf(LPURPLE+"loadRunMonitors:"+RED+" Fatal Monitor Count Stmt %s, exiting\n"+NCO, err)
			logger.Print("Fatal Monitor Count Stmt Query: " + err.Error())
			err2 = tx.Rollback()
			if err2 != nil {
				//fmt.Printf(LPURPLE+"loadRunMonitors:"+RED+" Fatal Monitor Count Stmt Rollback %s, exiting\n"+NCO, err2)
				logger.Print("Fatal Monitor Count Stmt Rollback: " + err2.Error())
				return err2
			}
			return err
		}
	}

	monitors = make(map[string]*Monitor, count)

	// Load monitors
	// Prepare statement
	rows, err := tx.Stmt(stmts["monitors_select_all_id"]).Query()
	if err != nil {
		//fmt.Printf(LPURPLE+"loadRunMonitors:"+RED+" Fatal Monitor All Ids Stmt Query %s, exiting\n"+NCO, err)
		logger.Print("Fatal Monitor All Ids Stmt Query: " + err.Error())
		err2 = tx.Rollback()
		if err2 != nil {
			//fmt.Printf(LPURPLE+"loadRunMonitors:"+RED+" Fatal Monitor All Ids Stmt Rollback %s, exiting\n"+NCO, err2)
			logger.Print("Fatal Monitor All Ids Stmt Rollback: " + err2.Error())
			return err2
		}
		return err
	}
	defer rows.Close()

	uuids := make([]string, 0)  // DEBUG

	// Collect monitor ids
	monids := make([]int, 0, count)
	for rows.Next() {
		monid = 0

		err = rows.Scan(&monid)
		if err != nil {
			//fmt.Printf(LPURPLE+"loadRunMonitors:"+RED+" Fatal Scan Monitor Id %s, exiting\n"+NCO, err)
			logger.Printf("Fatal Scan Monitor Id: %s", err.Error())
			// no need Rollback
			continue
		}
		if monid == 0 {
			continue
		}

		monids = append(monids, monid)
	}
	// CLOSE ROWS HERE!???
	//rows.Close()

	err = tx.Commit()
	if err != nil {
		//fmt.Printf(LPURPLE+"loadRunMonitors:"+RED+" Fatal Commit Get Ids Monitors %s, exiting\n"+NCO, err)
		logger.Printf("Fatal Commit Get Ids Monitors %s, exiting\n", err.Error())
		return err
	}

	// Load
	count = 0
	for _, monid = range monids {
		mon, err := loadMonitor(monid)
		if err != nil {
			logger.Print(err)
			continue
		}

		if mon.Active {
			run := true
			if (!mon.StopAt.IsZero()) && mon.StopAt.Before(time.Now()) {
				// condition for Duration or/and Amount mode (with deadline time)
				run = false
			} else if (mon.Amount > 0) && (mon.Counters.Done >= mon.Amount) {
				// condition only for Amount mode
				run = false
			}

			if run {
				err = mon.Run()
			} else {
				mon.Active = false
				err = mon.Save()
			}
			if err != nil {
				logger.Print(err)
			}
		}
		monitors[mon.UUID.String()] = mon

		count++
		uuids = append(uuids, mon.UUID.String())
	}

	uuids_list := strings.Join(uuids, ", ")
	//fmt.Printf(LPURPLE+"loadRunMonitors#%-23s:"+NCO+" Count Monitors %d Rows %s\n", time.Now().UTC().Format(time.RFC3339Nano), count, uuids_list)
	logger.Printf("Found %d monitors: [%s]\n", count, uuids_list)

	return nil
}

// initDB(config.Database) initialize database instance, loads them and run those having
// state active.
func initDB(dbconf DatabaseConf) (*sql.DB, error) {
	var dbo *sql.DB
	var err error

	logger.Print("Connect database...")

	switch dbconf.Type {
	case "sqlite":
		dbo, err = sql.Open("sqlite3", dbconf.Dsn)
		if err != nil {
			return nil, err
		}
		if dbo == nil {
			err = errors.New("Database in nil")
			if err != nil {
				return nil, err
			}
		}

		// TODO: Check connection (cannot use Ping() with sqlite, cannot test file exists instead of DSN string params)

	case "mysql":
		// Todo: add instantiate mysql database

		/*
		// Check connection
		err = dbo.Ping()
		if err != nil {
			return nil, err
		}
		*/

		fallthrough

	default:
		err = errors.New("Unknown database type")
		if err != nil {
			return dbo, err
		}
	}

	return dbo, err
}

func (mon *Monitor) Run() error {
	d := time.Duration(mon.Step) * time.Second
	t := time.NewTicker(d)
	mon.stop = make(chan int, 1)
	go func() {
		readings := make([](chan float64), len(mon.Values))
		for i := range readings {
			readings[i] = make(chan float64, 1)
		}
		vals := make([]interface{}, len(mon.Values)+1)
		for {
			select {
			case tm := <-t.C:
				if (!mon.StopAt.IsZero()) && mon.StopAt.Before(tm) {
					// condition for Duration or/and Amount mode (with deadline time)
					mon.Stop()
				} else if (mon.Amount > 0) && (mon.Counters.Done >= mon.Amount) {
					// condition only for Amount mode
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
				mon.incCounters(vals...)
				mon.Update(vals...)
			case <-mon.stop:
				return
			}
		}
	}()
	return nil
}

func (mon *Monitor) incCounters(vals ...interface{}) {
	// Check for errors
	is_err := false
	for i, v := range vals {
		if i == 0 {
			// Skip time
			continue
		}

		// Check error value
		detection, found := v.(float64)
		if !found {
			is_err = true
			break
		}
		if math.IsNaN(detection) {
			is_err = true
			break
		}
	}
	if len(vals) < 2 {
		is_err = true
	}

	//Increment counters
	mon.Counters.Done++
	if is_err {
		mon.Counters.Err++
	}
}

func (mon *Monitor) Update(vals ...interface{}) error {
	var err,err2 error

	is_err := false
	no_data := false
	
	// Works with mon copy
	// TODO: fix concurrent read access with sync.RWMutex, mon.RLock()
	monDBi, err := monitorToDB(mon)
	if err != nil {
		return err
	}

	if len(vals) < 2 {
		/*
		errf := fmt.Errorf("Update Error: no new detections for %s", monDBi.UUID))
		if errf != nil {
			return errf
		}
		*/
		is_err = true
		no_data = true
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	// Update Detections
	if !no_data {
		var nulltime time.Time
		tm, ok := vals[0].(time.Time)
		if !ok {
			tm = nulltime
		}

		det := DetectionDBItem{
			Id:             0,
			Exp_id:         monDBi.Exp_id,
			Mon_id:         monDBi.Id,
			Time:           tm.UTC().Format(time.RFC3339Nano),
			Sensor_id:      "",
			Sensor_val_id:  0,
			Detection:      0,
			Error:          "",  // TODO: remode old error field
		}

		sqlInsert := queries["_detections_insert_into"] + " VALUES "
		values := []interface{}{}
		for i, v := range vals {
			if i == 0 {
				// Skip time
				continue
			}

			sqlInsert += queries["_detections_insert_values"] + ","

			det_error := sql.NullString{String:"", Valid:false}
			det_value := sql.NullFloat64{Float64:0, Valid:false}
			var found bool

			// Check error value
			if det_value.Float64, found = v.(float64); !found {
				det_value.Float64 = math.NaN()
			}
			if math.IsNaN(det_value.Float64) {
				det_error.String = "NaN"
				det_error.Valid = true
				is_err = true
			} else {
				det_value.Valid = true
			}

			values = append(values,
				det.Exp_id,
				det.Mon_id,
				det.Time,
				monDBi.Values[i-1].Sensor,
				monDBi.Values[i-1].ValueIdx,
				det_value,
				det_error,
			)
		}
		sqlInsert = strings.TrimSuffix(sqlInsert, ",")
		//logger.Printf("Update: Debug sqlInsert: %s", sqlInsert)
		//logger.Printf("Update: Debug sqlInsert vals: %+v", values)
		// Prepare the statement
		stmt, err := tx.Prepare(sqlInsert)
		if err != nil {
			err2 = tx.Rollback()
			if err2 != nil {
				return err2
			}

			return err
		}

		// Execute
		//res, err := stmt.Exec(values...)
		_, err = stmt.Exec(values...)
		if err != nil {
			err2 = tx.Rollback()
			if err2 != nil {
				return err2
			}

			return err
		}

		//logger.Printf("Update: Inserted for Monitor %s Count Detections %d", monDBi.Id, res.RowsAffected())
	}

	// Update Counters
	// Execute
	if is_err {
		//res, err = tx.Stmt(stmts["monitors_counters_update_all_by_uuid"]).Exec(1, 1, monDBi.UUID)
		_, err = tx.Stmt(stmts["monitors_counters_update_all_by_uuid"]).Exec(1, 1, monDBi.UUID)
	} else {
		//res, err = tx.Stmt(stmts["monitors_counters_update_done_by_uuid"]).Exec(1, monDBi.UUID)
		_, err = tx.Stmt(stmts["monitors_counters_update_done_by_uuid"]).Exec(1, monDBi.UUID)
	}
	if err != nil {
		err2 = tx.Rollback()
		if err2 != nil {
			//fmt.Printf(LBLUE+"Save:"+RED+" Fatal Save Monitor Counters Update Stmt Rollback %s, exiting\n"+NCO, err2)
			//logger.Printf("Fatal Save Monitor Counters Update Stmt Rollback %s: %s: ", monDBi.UUID, err2.Error())
			return err2
		}
		return err
	}

	//logger.Printf("Update: Updated for Monitor %s Counters %d", monDBi.Id, res.RowsAffected())

	err = tx.Commit()
	if err != nil {
		//fmt.Printf(LBLUE+"Update:"+RED+" Fatal Commit Update Detections for %s: %s\n"+NCO, monDBi.UUID, err.Error())
		//logger.Printf("Update: Fatal Commit Update Detections for %s: %s", monDBi.UUID, err.Error())
		return err
	}

	//fmt.Printf(LBLUE+"Update %-23s:"+NCO+" insert detections\n", time.Now().Format("2006-01-02T15:04:05.999"))
	//logger.Printf("Update %-23s: insert detections for %s", time.Now().Format("2006-01-02T15:04:05.999"), monDBi.UUID)

	return nil
}

func (mon *Monitor) Stop() error {
	// TODO: fix concurrent read/write access with sync.RWMutex
	if !mon.Active {
		//logger.Print("Monitor " + mon.UUID.String() + " is inactive")
		return nil
	}

	select {
	case mon.stop <- 1:
	default:
	}

	mon.Active = false
	logger.Print("Monitor.Stop: ok (" + mon.UUID.String() + ")")

	return mon.Save()
}

func (mon *Monitor) SaveNew() error {
	var err,err2 error

	// Works with mon copy
	// TODO: fix concurrent read access with sync.RWMutex, mon.RLock()
	monDBi, err := monitorToDB(mon)
	if err != nil {
		return err
	}

	// as New Monitor
	monDBi.Id = 0

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	// Insert new monitor
	res, err := tx.Stmt(stmts["monitors_insert"]).Exec(
		monDBi.UUID,
		monDBi.Exp_id,
		monDBi.Setup_id,
		monDBi.Step,
		monDBi.Amount,
		monDBi.Duration,
		monDBi.Created,
		monDBi.StopAt,
		monDBi.Active,
	)
	if err != nil {
		err2 = tx.Rollback()
		if err2 != nil {
			//fmt.Printf(LBLUE+"SaveNew:"+RED+" Fatal Save Monitor Insert Stmt Rollback %s, exiting\n"+NCO, err2)
			//logger.Printf("Fatal Save Monitor Insert Stmt Rollback %s: %s: ", monDBi.UUID, err2.Error())
			return err2
		}
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		// not supported? ErrNotSupported?

		err2 = tx.Rollback()
		if err2 != nil {
			//fmt.Printf(LBLUE+"SaveNew:"+RED+" Fatal Save Monitor LastInsertId Stmt Rollback %s, exiting\n"+NCO, err2)
			//logger.Printf("Fatal Save Monitor LastInsertId Stmt Rollback %s: %s: ", monDBi.UUID, err2.Error())
			return err2
		}
		return err
	}

	// assign returned id
	// XXX: issue with overflow may be here, need int64 type in structs
	monDBi.Id = int(id)
	// TODO: fix concurrent write access with sync.RWMutex, mon.Lock(), but there are no runned mon yet, just creation
	mon.Id = int(id)

	// Save Monitor Values
	// only once
	sqlInsert := queries["_monitors_values_replace_into"] + " VALUES "
	values := []interface{}{}
	for _, monv := range monDBi.Values {
		sqlInsert += queries["_monitors_values_replace_values"] + ","
		values = append(values,
			monDBi.UUID,
			monv.Name,
			monv.Sensor,
			monv.ValueIdx,
		)
	}
	sqlInsert = strings.TrimSuffix(sqlInsert, ",")

	// Prepare the statement
	stmt, err := tx.Prepare(sqlInsert)
	if err != nil {
		err2 = tx.Rollback()
		if err2 != nil {
			//fmt.Printf(LBLUE+"SaveNew:"+RED+" Fatal Save Monitor Values Insert Prepare Rollback %s, exiting\n"+NCO, err2)
			//logger.Printf("Fatal Save Monitor Values Insert Prepare Rollback %s: %s: ", monDBi.UUID, err2.Error())
			return err2
		}
		return err
	}

	// Execute
	//res, err := stmt.Exec(values...)
	_, err = stmt.Exec(values...)
	if err != nil {
		err2 = tx.Rollback()
		if err2 != nil {
			//fmt.Printf(LBLUE+"SaveNew:"+RED+" Fatal Save Monitor Values Insert Exec Rollback %s, exiting\n"+NCO, err2)
			//logger.Printf("Fatal Save Monitor Values Insert Exec Rollback %s: %s: ", monDBi.UUID, err2.Error())
			return err2
		}
		return err
	}

	//logger.Printf("SaveNew: Inserted for Monitor %s Count Values %d", monDBi.UUID, res.RowsAffected())

	// Save Monitor Counters
	// only once
	// Execute
	//res, err := tx.Stmt(stmts["monitors_counters_replace"]).Exec(monDBi.UUID, 0, 0)
	_, err = tx.Stmt(stmts["monitors_counters_replace"]).Exec(monDBi.UUID, 0, 0)
	if err != nil {
		err2 = tx.Rollback()
		if err2 != nil {
			//fmt.Printf(LBLUE+"SaveNew:"+RED+" Fatal Save Monitor Counters Insert Stmt Rollback %s, exiting\n"+NCO, err2)
			//logger.Printf("Fatal Save Monitor Counters Insert Stmt Rollback %s: %s: ", monDBi.UUID, err2.Error())
			return err2
		}
		return err
	}

	//logger.Printf("SaveNew: Inserted for Monitor %s Counters %d", monDBi.UUID, res.RowsAffected())

	err = tx.Commit()
	if err != nil {
		//fmt.Printf(LBLUE+"SaveNew:"+RED+" Fatal Commit Save Monitor %s\n"+NCO, monDBi.UUID, err)
		//logger.Printf("Fatal Commit Save Monitor %s: %s", monDBi.UUID, err.Error())
		return err
	}

	//fmt.Printf(LBLUE+"SaveNew %-23s:"+NCO+" saved monitor %s\n", time.Now().Format("2006-01-02T15:04:05.999"), monDBi.UUID)
	//logger.Printf("SaveNew %-23s: saved monitor %s", time.Now().Format("2006-01-02T15:04:05.999"), monDBi.UUID)

	return nil
}

func (mon *Monitor) Save() error {
	var err,err2 error

	// Works with mon copy
	// TODO: fix concurrent read access with sync.RWMutex, mon.RLock()
	monDBi, err := monitorToDB(mon)
	if err != nil {
		return err
	}

	// only Edit Monitor
	if monDBi.Id == 0 {
		return errors.New("monitor not saved, incorrect id");
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	// Update monitor
	_, err = tx.Stmt(stmts["monitors_replace"]).Exec(
		monDBi.Id,
		monDBi.UUID,
		monDBi.Exp_id,
		monDBi.Setup_id,
		monDBi.Step,
		monDBi.Amount,
		monDBi.Duration,
		monDBi.Created,
		monDBi.StopAt,
		monDBi.Active,
	)
	if err != nil {
		err2 = tx.Rollback()
		if err2 != nil {
			//fmt.Printf(LBLUE+"Save:"+RED+" Fatal Save Monitor Replace Stmt Rollback %s, exiting\n"+NCO, err2)
			//logger.Printf("Fatal Save Monitor Replace Stmt Rollback %s: %s: ", monDBi.UUID, err2.Error())
			return err2
		}
		return err
	}

	err = tx.Commit()
	if err != nil {
		//fmt.Printf(LBLUE+"Save:"+RED+" Fatal Commit Save Monitor %s\n"+NCO, monDBi.UUID, err)
		//logger.Printf("Fatal Commit Save Monitor %s: %s", monDBi.UUID, err.Error())
		return err
	}

	//fmt.Printf(LBLUE+"Save %-23s:"+NCO+" update monitor %s\n", time.Now().Format("2006-01-02T15:04:05.999"), monDBi.UUID)
	//logger.Printf("Save %-23s: update monitor %s", time.Now().Format("2006-01-02T15:04:05.999"), monDBi.UUID)

	return nil
}

func (mon *Monitor) Info() (*MonitorInfo, error) {
	var err, err2 error

	// Works with mon copy
	// TODO: fix concurrent read access with sync.RWMutex, mon.RLock()
	monDBi, err := monitorToDB(mon)
	if err != nil {
		return nil, err
	}
	created := mon.Created
	stopat := mon.StopAt

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}

	// Get counters
	row := tx.Stmt(stmts["monitors_counters_select_by_uuid"]).QueryRow(monDBi.UUID)
	monuuid := ""
	counters := MonCounters{}
	// XXX: can use monDBi.Counters, but its in mon in memory, mon counters may be not equal to stored to db values
	err = row.Scan(&monuuid, &counters.Done, &counters.Err)
	if err != nil {
		if err == sql.ErrNoRows {
			// there were no rows, but otherwise no error occurred
			counters.Done = 0
			counters.Err = 0
		} else {
			//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Select Monitors Counters Stmt %s, exiting\n"+NCO, err)
			logger.Print("Fatal Select Monitors Counters Stmt Query: " + err.Error())
			err2 = tx.Rollback()
			if err2 != nil {
				//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Select Monitors Counters Stmt Rollback %s, exiting\n"+NCO, err2)
				logger.Print("Fatal Select Monitors Counters Stmt Rollback: " + err2.Error())
				return nil, err2
			}
			return nil, err
		}
	}

	// Count grouped detections
	row = tx.Stmt(stmts["detections_count_by_monitor_grouptime"]).QueryRow(monDBi.Id)
	var alen uint = 0
	err = row.Scan(&alen)
	if err != nil {
		if err == sql.ErrNoRows {
			// there were no rows, but otherwise no error occurred
			alen = 0
		} else {
			//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Detections Grouped Count Stmt %s, exiting\n"+NCO, err)
			logger.Print("Fatal Detections Grouped Count Stmt Query: " + err.Error())
			err2 = tx.Rollback()
			if err2 != nil {
				//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Detections Grouped Count Stmt Rollback %s, exiting\n"+NCO, err2)
				logger.Print("Fatal Detections Grouped Count Stmt Rollback: " + err2.Error())
				return nil, err2
			}
			return nil, err
		}
	}

	// Get last detection time
	row = tx.Stmt(stmts["detections_select_last_time_by_monitor"]).QueryRow(monDBi.Id)
	var nulltime time.Time
	lasttxt, last := "", nulltime
	err = row.Scan(&lasttxt)
	if err != nil {
		if err == sql.ErrNoRows {
			// there were no rows, but otherwise no error occurred
			lasttxt = ""
		} else {
			//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Detections Last Time Stmt %s, exiting\n"+NCO, err)
			logger.Print("Fatal Detections Last Time Stmt Query: " + err.Error())
			err2 = tx.Rollback()
			if err2 != nil {
				//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Detections Last Time Stmt Rollback %s, exiting\n"+NCO, err2)
				logger.Print("Fatal Detections Last Time Stmt Rollback: " + err2.Error())
				return nil, err2
			}
			return nil, err
		}
	}
	if lasttxt != "" {
		last, _ = time.Parse(time.RFC3339Nano, lasttxt)
	}

	n := 1  // number of archives, only one archive now, no step multiplied stores on Step*2, Step*4, Step*16, and etc.
	ai := make([]ArchiveInfo, n)
	for i := range ai {
		ai[i] = ArchiveInfo{
			monDBi.Step, // archive data step
			alen,
		}
	}

	// Get Values data
	var vlen uint
	vi := make([]MonValueInfo, len(monDBi.Values))
	for i := range vi {
		// Count separate Values
		vlen = 0
		row = tx.Stmt(stmts["detections_count_by_monitor_sensor"]).QueryRow(
			monDBi.Id,
			monDBi.Values[i].Sensor,
			monDBi.Values[i].ValueIdx,
		)
		err = row.Scan(&vlen)
		if err != nil {
			if err == sql.ErrNoRows {
				// there were no rows, but otherwise no error occurred
				vlen = 0
			} else {
				//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Detections Grouped Sensor Count Stmt %s, exiting\n"+NCO, err)
				logger.Print("Fatal Detections Grouped Sensor Count Stmt Query: " + err.Error())
				err2 = tx.Rollback()
				if err2 != nil {
					//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Detections Grouped Sensor Count Stmt Rollback %s, exiting\n"+NCO, err2)
					logger.Print("Fatal Detections Grouped Sensor Count Stmt Rollback: " + err2.Error())
					return nil, err2
				}
				return nil, err
			}
		}

		vi[i] = MonValueInfo{
			monDBi.Values[i].Name,
			monDBi.Values[i].Sensor,
			monDBi.Values[i].ValueIdx,
			vlen,
		}
	}

	err = tx.Commit()
	if err != nil {
		//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Commit Detections Grouped Sensor Count %s, exiting\n"+NCO, err)
		logger.Printf("Fatal Commit Monitor Info %s, exiting\n", err.Error())
		return nil, err
	}

	mi := &MonitorInfo{
		monDBi.Active,
		created,
		stopat,
		last,
		monDBi.Amount,
		monDBi.Duration,
		counters,
		ai,
		vi,
	}
	return mi, nil
}

func (mon *Monitor) Fetch(start, end time.Time, step time.Duration) (*FetchResultDB, error) {
	var err, err2 error

	// Works with mon copy
	// TODO: fix concurrent read access with sync.RWMutex, mon.RLock()
	monDBi, err := monitorToDB(mon)
	if err != nil {
		return nil, err
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}

	fr := &FetchResultDB{
		Filename: config.Database.Type + ":" + config.Database.Dsn,  // XXX: old, not used (only for RRD)
		Cf:       "AVERAGE",  // XXX: not AVERAGE, just ABSOLUTE now, not used (only for RRD)
		Start:    start,
		End:      end,
		Step:     time.Duration(monDBi.Step) * time.Second,  // XXX: old, not used (only for RRD)
		DsNames:  make([]string, len(monDBi.Values)),
		RowCnt:   0,
		DsData:   make([]*FetchResultDBItem, 0),
	}
	//fr.DsNames = make([]string, len(monDBi.Values))
	//fr.DsData = make([]*FetchResultDBItem, 0)

	var sensor_val_id int
	var tm, sensor_id string
	var detection sql.NullFloat64
	var derror    sql.NullString

	// Load detections
	var rows *sql.Rows
	if start.IsZero() && end.IsZero() {
		rows, err = tx.Stmt(stmts["detections_select_by_monitor"]).Query(
			monDBi.Id,
		)
	} else if start.IsZero() {
		rows, err = tx.Stmt(stmts["detections_select_by_monitor_time_to"]).Query(
			monDBi.Id,
			end.UTC().Format(time.RFC3339Nano),
		)
	} else if end.IsZero() {
		rows, err = tx.Stmt(stmts["detections_select_by_monitor_time_from"]).Query(
			monDBi.Id,
			start.UTC().Format(time.RFC3339Nano),
		)
	} else {
		rows, err = tx.Stmt(stmts["detections_select_by_monitor_time_range"]).Query(
			monDBi.Id,
			start.UTC().Format(time.RFC3339Nano),
			end.UTC().Format(time.RFC3339Nano),
		)
	}

	if err != nil {
		//fmt.Printf(LPURPLE+"Fetch:"+RED+" Fatal Detections Select Time Range Stmt Query %s, exiting\n"+NCO, err)
		logger.Print("Fatal Detections Select Time Range Stmt Query: " + err.Error())
		err2 = tx.Rollback()
		if err2 != nil {
			//fmt.Printf(LPURPLE+"Fetch:"+RED+" Fatal Detections Select Time Range Rollback %s, exiting\n"+NCO, err2)
			logger.Print("Fatal Detections Select Time Range Stmt Rollback: " + err2.Error())
			return nil, err2
		}
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		err = rows.Scan(&tm, &sensor_id, &sensor_val_id, &detection, &derror)
		if err != nil {
			//fmt.Printf(LPURPLE+"Fetch:"+RED+" Fatal Detections Select Time Range Scan, exiting\n"+NCO, err)
			logger.Printf("Fatal Detections Select Time Range Scan: %s", err.Error())
			err2 = tx.Rollback()
			if err2 != nil {
				//fmt.Printf(LPURPLE+"Fetch:"+RED+" Fatal Detections Select Time Range Scan Rollback %s, exiting\n"+NCO, err2)
				logger.Printf("Fatal Detections Select Time Range Scan Rollback: %s", err2.Error())
				return nil, err2
			}
			return nil, err
		}

		t, _ := time.Parse(time.RFC3339Nano, tm)
		
		// Link with DsNames by name
		// Search Name by unique sensor info
		name := ""
		for _, v := range monDBi.Values {
			if v.Sensor == sensor_id && v.ValueIdx == sensor_val_id {
				name = v.Name
				break
			}
		}

		// Convert non valid to NaN value
		if !detection.Valid {
			detection.Valid = true
			detection.Float64 = math.NaN()
		}
		// Convert non valid error to empty string value
		if !derror.Valid {
			derror.Valid = true
			derror.String = ""
		}

		fr.DsData = append(fr.DsData, &FetchResultDBItem{t, name, detection.Float64, derror.String});
	}

	err = tx.Commit()
	if err != nil {
		//fmt.Printf(LPURPLE+"Fetch:"+RED+" Fatal Commit Detections Select Time Range %s, exiting\n"+NCO, err)
		logger.Printf("Fatal Commit Detections Select Time Range %s, exiting\n", err.Error())
		return nil, err
	}

	fr.RowCnt = len(fr.DsData)

	for i := range fr.DsNames {
		fr.DsNames[i] = monDBi.Values[i].Name;
	}

	return fr, err
}

func (mon *Monitor) Remove(wdata bool) error {
	var err,err2 error
	var errcnt uint = 0

	if mon.Active {
		err = mon.Stop()
		if err != nil {
			logger.Print("error stopping monitor being removed: " + err.Error())
		}
	}
	delete(monitors, mon.UUID.String())

	// Works with mon copy
	// TODO: fix concurrent read access with sync.RWMutex, mon.RLock()
	monDBi, err := monitorToDB(mon)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	// Delete monitor detections data
	if wdata {
		_, err = tx.Stmt(stmts["detections_delete_by_monitor"]).Exec(monDBi.Id)
		if err != nil {
			errcnt++
			logger.Print("error removing monitor data: " + err.Error())
		}
	}

	// Delete monitor values
	//mon.Values = nil
	_, err = tx.Stmt(stmts["monitors_values_delete_by_uuid"]).Exec(monDBi.UUID)
	if err != nil {
		errcnt++
		logger.Print("error removing monitor values: " + err.Error())
	}

	// Delete monitor counters
	//mon.Counters.Done = 0
	//mon.Counters.Err = 0
	_, err = tx.Stmt(stmts["monitors_counters_delete_by_uuid"]).Exec(monDBi.UUID)
	if err != nil {
		errcnt++
		logger.Print("error removing monitor counters: " + err.Error())
	}

	// Delete monitor
	_, err = tx.Stmt(stmts["monitors_delete_by_id"]).Exec(monDBi.Id)
	if err != nil {
		errcnt++
		logger.Print("error removing monitor configuration: " + err.Error())
	}

	if errcnt > 0 {
		err2 = tx.Rollback()
		if err2 != nil {
			//fmt.Printf(LPURPLE+"Remove:"+RED+" Fatal Rollback Monitor Remove %s, exiting\n"+NCO, err2)
			logger.Printf("Fatal Rollback Monitor Remove: %s", err2.Error())
		}
	} else {
		err = tx.Commit()
		if err != nil {
			//fmt.Printf(LPURPLE+"Remove:"+RED+" Fatal Commit Monitor Remove %s, exiting\n"+NCO, err)
			logger.Printf("Fatal Commit Monitor Remove %s, exiting\n", err.Error())
		}
	}

	// Result error
	if errcnt > 0 {
		err = fmt.Errorf("error removing monitor: %d : %s", monDBi.Id, monDBi.UUID)
	}
	return err
}

func runStrobe(monDBi *MonitorDBItem, check bool) error {
	// Use monitor data (also sensors) to make one detections strobe

	if check {
		// Check values
		if len(monDBi.Values) == 0 {
			return errors.New("no sensors selected")
		}
		// Check that values are available
		for _, v := range monDBi.Values {
			if pluggedSensors[v.Sensor] == nil {
				return errors.New("no sensor '" + v.Sensor + "' connected")
			}
			if len(pluggedSensors[v.Sensor].Values) <= v.ValueIdx {
				return fmt.Errorf("no value %d for sensor '%s' available", v.ValueIdx, v.Sensor)
			}
		}
	}

	go func() {
		readings := make([](chan float64), len(monDBi.Values))
		for i := range readings {
			readings[i] = make(chan float64, 1)
		}
		vals := make([]interface{}, len(monDBi.Values)+1)
		for i, v := range monDBi.Values {
			go getSerData(v.Sensor, v.ValueIdx, readings[i])
		}
		vals[0] = time.Now()
		for i, c := range readings {
			vals[i+1] = <-c
		}
		updateStrob(monDBi, vals...)
	}()
	return nil
}

func updateStrob(monDBi *MonitorDBItem, vals ...interface{}) error {
	var err,err2 error

	if len(vals) < 2 {
		//return fmt.Errorf("Update Strobe Error: no new detections for %s", monDBi.UUID)
		return nil
	}

	var nulltime time.Time
	tm, ok := vals[0].(time.Time)
	if !ok {
		tm = nulltime
	}

	det := DetectionDBItem{
		Id:             0,
		Exp_id:         monDBi.Exp_id,
		Mon_id:         monDBi.Id,
		Time:           tm.UTC().Format(time.RFC3339Nano),
		Sensor_id:      "",
		Sensor_val_id:  0,
		Detection:      0,
		Error:          "",  // TODO: remode old error field
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	sqlInsert := queries["_detections_insert_into"] + " VALUES "
	values := []interface{}{}
	for i, v := range vals {
		if i == 0 {
			// Skip time
			continue
		}

		sqlInsert += queries["_detections_insert_values"] + ","

		det_error := sql.NullString{String:"", Valid:false}
		det_value := sql.NullFloat64{Float64:0, Valid:false}
		var found bool

		// Check error value
		if det_value.Float64, found = v.(float64); !found {
			det_value.Float64 = math.NaN()
		}
		if math.IsNaN(det_value.Float64) {
			det_error.String = "NaN"
			det_error.Valid = true
		} else {
			det_value.Valid = true
		}

		values = append(values,
			det.Exp_id,
			det.Mon_id,
			det.Time,
			monDBi.Values[i-1].Sensor,
			monDBi.Values[i-1].ValueIdx,
			det_value,
			det_error,
		)
	}
	sqlInsert = strings.TrimSuffix(sqlInsert, ",")
	//logger.Printf("Update Strobe: Debug sqlInsert: %s", sqlInsert)
	//logger.Printf("Update Strobe: Debug sqlInsert vals: %+v", values)
	// Prepare the statement
	stmt, err := tx.Prepare(sqlInsert)
	if err != nil {
		err2 = tx.Rollback()
		if err2 != nil {
			return err2
		}

		return err
	}

	// Execute
	//res, err := stmt.Exec(values...)
	_, err = stmt.Exec(values...)
	if err != nil {
		err2 = tx.Rollback()
		if err2 != nil {
			return err2
		}

		return err
	}

	//logger.Printf("Update Strobe: Inserted for Monitor %s Count Detections %d", monDBi.Id, res.RowsAffected())

	err = tx.Commit()
	if err != nil {
		//fmt.Printf(LBLUE+"Update Strobe:"+RED+" Fatal Commit Update Detections for %s: %s\n"+NCO, monDBi.UUID, err)
		//logger.Printf("Update Strobe: Fatal Commit Update Detections for %s: %s", monDBi.UUID, err.Error())
		return err
	}

	//fmt.Printf(LBLUE+"Update Strobe %-23s:"+NCO+" insert detections\n", time.Now().Format("2006-01-02T15:04:05.999"))
	//logger.Printf("Update Strobe %-23s: insert detections for %s", time.Now().Format("2006-01-02T15:04:05.999"), monDBi.UUID)

	return nil
}

func newMonitor(opts *MonitorOpts) (*Monitor, error) {
	// may be infinite monitoring
	if (!opts.StopAt.IsZero()) && opts.StopAt.Before(time.Now()) {
		err := errors.New("monitor stop time is in the past")
		return nil, err
	}
	vals := make([]MonValue, len(opts.Values))
	for i, v := range opts.Values {
		ok, errcode := valueAvailable(v.Sensor, v.ValueIdx)
		if !ok {
			switch errcode {
			case 1:
				err := errors.New("no sensor '" + v.Sensor + "' connected")
				return nil, err

			case 2:
				err := fmt.Errorf("no value %d for sensor '%s' available", v.ValueIdx, v.Sensor)
				return nil, err

			default:
				err := errors.New("Wrong sensor spec")
				return nil, err
			}
		}

		vals[i] = MonValue{
			pluggedSensors[v.Sensor].Values[v.ValueIdx].Name + strconv.Itoa(i),
			v.Sensor,
			v.ValueIdx,
			pluggedSensors[v.Sensor].Values[v.ValueIdx].Type,
			0,
		}
	}

	mon := Monitor{
		0,
		uuid.NewRandom(),
		opts.Exp_id,
		opts.Setup_id,
		opts.Step,
		opts.Count,
		opts.Duration,
		time.Now(),
		opts.StopAt,
		true,
		nil,
		vals,
		MonCounters{0,0},
	}

	return &mon, nil
}

func createRunMonitor(opts *MonitorOpts) (*Monitor, error) {
	mon, err := newMonitor(opts)
	if err != nil {
		return mon, err
	}
	logger.Print("createRunMonitor: newMonitor: ok")
	err = mon.SaveNew()
	if err != nil {
		return mon, err
	}
	logger.Print("createRunMonitor: mon.SaveNew: ok")
	err = mon.Run()
	if err != nil {
		return mon, err
	}
	logger.Print("createRunMonitor: mon.Run: ok")

	monitors[mon.UUID.String()] = mon

	return mon, nil
}
