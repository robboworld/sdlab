# sdlab
SDLab backend service. Used for getting data from sensors and monitoring with saving detections to RRD database. Provide special API for frontend socket/tcp requests.

## JSON RPC SDLab backend API

Testing write requests and receive responses with socket connection using socat:

    $ sudo socat UNIX:/var/run/sdlab.sock -

JSON Request format:

``` json
    {"jsonrpc":"2.0","method":"Lab.METHOD","params":[PARAMS],"id":0}
```

where
    jsonrpc - request type/version,
    method - called method (ex. Lab.METHOD),
    params - method arguments (object, array, string and etc.) or empty (empty array),
    id - request id.

JSON Response format:

``` json
    {"id":0,"result":RESULT,"error":ERROR}
```

where
    id - request id,
    result - returned results (array, object, string, boolean and etc.),
    error - string with error description if returned, if no error empty (null).


### Methods. Simple data API

1. Lab.GetData
   Read data from single sensor, get single value.
   Params:
       object  objects with sensor-value info:
                Sensor - sensor identifier,
                ValueIdx - value index.
   Returns:
       object  with:
                Time - time in RFC3339 format with TZ and nanoseconds,
                Reading - value(ints, floats and etc.) at this Time.
              Empty on error.
   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.GetData","params":[{"Sensor":"bmp085-1:77","ValueIdx":1}],"id":0}
```
   Response:
``` json
    {"id":0,"result":{"Time":"2016-08-25T13:18:58.925888913+03:00","Reading":299.65},"error":null}
```
   Response (error):
``` json
    {"id":0,"result":null,"error":"Cannot read file '/sys/bus/i2c/devices/i2c-1/1-0077/temp0_input': read /sys/bus/i2c/devices/i2c-1/1-0077/temp0_input: communication error on send"}
```


### Methods. Series API

1. Lab.StartSeries
   Start series detections, store detections data in process memory, identified by uuid,
   Maximum detections buffer capacity is specified in application config file.
   Maximum simultaneously series count (pool length) is specified in config file also.
   Returns:
       string  uuid on success, null on error (max available series in pool, unknown sensor and etc.)

   Example - series length = 3, 1 sensor, 1 value, period = 1 sec.
   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.StartSeries","params":[{"Values":[{"Sensor":"bmp085-1:77","ValueIdx":0}
        ],"Period":1000000000,"Count":3}],"id":0}
```
   Response:
``` json
    {"id":0,"result":"835047db-3d85-4a50-8d9e-4f9fc23dd2ac","error":null}
```

   Example - series length = 10, 1 sensor, 2 values, period = 15 sec:
   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.StartSeries","params":[{"Values":[{"Sensor":"bmp085-1:77","ValueIdx":0},{"Sensor":"bmp085-1:77","ValueIdx":1}
        ],"Period":15000000000,"Count":10}],"id":0}
```
   Response:
``` json
    {"id":0,"result":"115047db-3d85-4a23-8d9e-4f9fcc3dd2ac","error":null}
```

2. Lab.StopSeries
   Stop series detection by uuid.
   Returns:
       bool  true success, false or null on error

   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.StopSeries","params":["ec9a44d7-11e6-4b6a-af6c-897f7884f985"],"id":0}
```
   Response:
``` json
    {"id":0,"result":true,"error":null}
```

3. Lab.GetSeries
   Get series detection by uuid. For running and already stopped series also.
   Carefully, detections data is pushed out from memory buffer.
   Returns:
       array  array of objects with
                Time - time in RFC3339 format with TZ and nanoseconds,
                Readings - array of values(ints, floats and etc.) at this Time.
              Empty on error.

   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.GetSeries","params":["ec9a44d7-11e6-4b6a-af6c-897f7884f985"],"id":0}
```
   Response:
``` json
    {"id":0,"result":[
        {"Time":"2016-08-15T13:38:00.31759214+03:00","Readings":[100284,298.54999999999995]},
        {"Time":"2016-08-15T13:38:15.317576023+03:00","Readings":[100281,298.54999999999995]},
        ...
        {"Time":"2016-08-15T13:40:00.317610447+03:00","Readings":[100283,298.65]},
        {"Time":"2016-08-15T13:40:15.317576872+03:00","Readings":[100288,298.65]}],"error":null}
```

4. Lab.ListSeries
   Get list of registered series detection (runned and stopped).
   Returns:
       array  array of objects with
                UUID - string series id,
                Stop - bool, true if stopped by request, else false,
                Finished - bool, true if finished (made requested detections count), else false.
                Len - int, number of finished detections, may be larger than buffer length (older data is pushed out)
              Empty on error.

   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.ListSeries","params":[],"id":0}
```
   Response:
``` json
    {"id":0,"result":[
        {"UUID":"835047db-3d85-4a50-8d9e-4f9fc23dd2ac","Stop":false,"Finished":true,"Len":3},
        {"UUID":"1bfe4c62-7e91-45b3-8313-12a28d2e3f4c","Stop":false,"Finished":false,"Len":2}],"error":null}
```

5. Lab.StopSeries
   Stop series detection by uuid.
   Returns:
       bool  true on success, false or null on error (if stopped already or not exists and etc.)

   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.StopSeries","params":["ec9a44d7-11e6-4b6a-af6c-897f7884f985"],"id":0}
```
   Response:
``` json
    {"id":0,"result":true,"error":null}
```

6. Lab.RemoveSeries
   Remove series detection by uuid from memory pool. Release pool for new series.
   Returns:
       bool  true on success, false or null on error (if not exists and etc.)

   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.RemoveSeries","params":["fa20547a-71ba-4f24-9c72-93b0bf3ebbc8"],"id":0}
```
   Response:
``` json
    {"id":0,"result":true,"error":null}
```

7. Lab.CleanSeries
   Remove ALL series detection from memory pool. Release pool for new series.
   Returns:
       bool  true on success, false or null on error

   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.CleanSeries","params":[],"id":0}
```
   Response:
``` json
    {"id":0,"result":true,"error":null}
```


### Methods. Monitoring API

1. Lab.StartMonitor
   Start long monitoring with storing data to external storage as RRD, or SQL-liked database.
   Database type and parameters is specified in application config file.
   Two stop conditions supported:
       - stop by detections count, if set Count > 0,
       - stop by time, if StopAt is not zero time (00001-01-01T00:00:00Z - January 1, year 1, 00:00:00.000000000 UTC),
         if zero time - run infinite or until reached Count or until stop command sended.
   Parameter Duration is not used as stop condition, just must set to cache in monitor info, 0 if not used.
   Please calculate StopAt for stop by time condition.

   Returns:
       string  uuid on success, null on error (unknown sensor and etc.)

   Example - monitor for experiment = 1, sensors setup = 1, detections count = 0, step = 5sec, duration = 100sec, 
             stop at 2016-08-16T14:50:00Z, 1 sensor, 1 value.
   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.StartMonitor","params":[
        {"Exp_id":1,"Setup_id":1,"Step":5,"Count":0,"Duration":100,"StopAt":"2016-08-16T14:50:00Z","Values":[
            {"Sensor":"bmp085-1:77","ValueIdx":0}]
        }],"id":0}
```
   Response:
``` json
    {"id":0,"result":"835047db-3d85-4a50-8d9e-4f9fc23dd2ac","error":null}
```

   Example - monitor for experiment = 1, sensors setup = 1, detections count = 20, step = 1sec, no stop at, 1 sensor, 2 values.
   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.StartMonitor","params":[
        {"Exp_id":1,"Setup_id":1,"Step":1,"Count":20,"Duration":20,"StopAt":"00001-01-01T00:00:00Z","Values":[
            {"Sensor":"bmp085-1:77","ValueIdx":0},
            {"Sensor":"bmp085-1:77","ValueIdx":1}]
        }],"id":0}

```
   Response:
``` json
    {"id":0,"result":"857e2ec6-1099-4879-aa06-0f65a24dad2c","error":null}
```

2. Lab.StopMonitor
   Stop monitor by uuid.
   Returns:
       bool  true success, false or null on error

   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.StopMonitor","params":["857e2ec6-1099-4879-aa06-0f65a24dad2c"],"id":0}
```
   Response:
``` json
    {"id":0,"result":true,"error":null}
```

3. Lab.ListMonitors
   Get list of monitoring processes (runned and stopped).
   Returns:
       array  array of objects with
                Active - bool, true if monitoring is active, else false,
                UUID - string monitor uuid,
                Created - string, creation time in RFC3339 format with TZ and nanoseconds,
                StopAt - string, stop at restriction time in RFC3339 format with TZ and nanoseconds,
                Values - array of objects with sensor values info:
                         Name - sensor name,
                         Sensor - sensor identifier,
                         ValueIdx - value index.
              Empty on error.

   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.ListMonitors","params":[],"id":0}
```
   Response:
``` json
    {"id":0,"result":[
        {"Active":false,"UUID":"857e2ec6-1099-4879-aa06-0f65a24dad2c","Created":"2016-08-17T16:18:24.258780114+03:00", "StopAt":"2016-08-17T13:25:00Z",
         "Values":[
            {"Name":"pressure0","Sensor":"bmp085-1:77","ValueIdx":0},
            {"Name":"temperature1","Sensor":"bmp085-1:77","ValueIdx":1}]},
        {"Active":false,"UUID":"ac19da70-85bc-4b0f-8513-5b97d2cadb27","Created":"2016-08-16T21:21:28.426346079+03:00","StopAt":"2016-08-16T18:22:00Z",
         "Values":[
            {"Name":"pressure0","Sensor":"bmp085-1:77","ValueIdx":0}]},
        {"Active":false,"UUID":"b26d1988-b6b5-4b6d-80a6-01bc43e53aab","Created":"2016-08-16T21:21:33.534725373+03:00","StopAt":"2016-08-16T18:25:00Z",
         "Values":[
            {"Name":"pressure0","Sensor":"bmp085-1:77","ValueIdx":0},
            {"Name":"temperature1","Sensor":"bmp085-1:77","ValueIdx":1}]}
        ],"error":null}
```

4. Lab.GetMonInfo
   Get monitoring process info by uuid.
   Returns:
       object with
                Active - bool, true if monitoring is active, else false,
                Created - string, creation time in RFC3339 format with TZ and nanoseconds,
                StopAt - string, "stop at" time restriction in RFC3339 format with TZ and nanoseconds,
                Last - string, last detection time in RFC3339 format with TZ and nanoseconds,
                Amount - uint, stop at count condition (0 if not used),
                Duration - uint, monitoring duration cached in monitor (0 if not used),
                Counters - object with currect counters (Done - all done detectios, Err - failed detections from all)
                Archives - array of objects (Step, Len) with data storing steps info, 
                  if storage support returned multiple steps (as Step x 1, Step x 4, Step x 16, end etc., in seconds), 
                  default only one Step x 1, returned also Len of data strobes already saved by each Step,
                Values - array of objects with sensor values info:
                         Name - sensor name,
                         Sensor - sensor identifier,
                         ValueIdx - value index,
                         Len - detections made by this sensor and value (by default Step)
              Empty on error.
   Returns:
       string  uuid on success, null on error (unknown sensor and etc.)

   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.GetMonInfo","params":["857e2ec6-1099-4879-aa06-0f65a24dad2c"],"id":0}
```
   Response:
``` json
    {"id":0,"result":
        {"Active":false,"Created":"2016-08-17T16:18:24.258780114+03:00","StopAt":"2016-08-17T13:25:00Z","Last":"2016-08-17T16:21:14.305Z",
         "Amount":20,"Duration":444, 
         "Counters":{"Done":170,"Err":4},
         "Archives":[{"Step":1,"Len":170}],
         "Values":[
             {"Name":"pressure0","Sensor":"bmp085-1:77","ValueIdx":0,"Len":170},
             {"Name":"temperature1","Sensor":"bmp085-1:77","ValueIdx":1,"Len":170}]
        },"error":null}
```

5. Lab.RemoveMonitor
   Remove monitoring by uuid from memory and storage register at all.
   If WithData is true also removes all detections data made by this monitoring.
   Params:
       object  
           UUID  string,
           WithData  bool
   Returns:
       bool  true on success, false or null on error (if not exists and etc.)

   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.RemoveMonitor","params":[{"UUID":"857e2ec6-1099-4879-aa06-0f65a24dad2c","WithData":true}],"id":0}
```
   Response:
``` json
    {"id":0,"result":true,"error":null}
```

6. Lab.StrobeMonitor
   Create one strobe of detections for monitoring and update database immediately.
   Can be used by exists monitoring if specified UUID, or standalone for custom sensors and values set.
   Params:
       object  
           UUID  string, monitor UUID if use exists monitor sensors configuration (than Opts, OptsStrict may be omitted),
           Opts  object with monitoring info and sensors setup, MUST be used if empty UUID, else ignored:
                Exp_id int - experiment id,
                Setup_id int - setup id (NOT USED),
                Step int - (NOT USED),
                Count int - number of detections, always 1 (NOT USED),
                Duration int - duration, always 0 (NOT USED),
                StopAt int - time (NOT USED, but MUST be not null, can be zero time = 0001-01-01T00:00:00Z),
                Values - array of objects with sensor values info:
                         Sensor - sensor identifier,
                         ValueIdx - value index,
           OptsStrict  bool, true to check if sensors and values exists and valid, false to skip checking and save NaN values on sensor error,
                can be used with Opts, default False.
   Returns:
       bool  true on success, false or null on error (if not exists monitor or sensors error in strict mode and etc.)

   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.StrobeMonitor","params":[{"UUID":"3358c035-6e58-4a78-bb5b-7a6e6c2b2d67"}],"id":0}
```
   Response:
``` json
    {"id":0,"result":true,"error":null}
```

   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.StrobeMonitor","params":[
        {"UUID":"","Opts":{"Exp_id":2,"Setup_id":2,"Step":1,"Count":1,"Duration":0,"StopAt":"0001-01-01T00:00:00Z",
         "Values":[{"Sensor":"bmp085-1:77","ValueIdx":0},{"Sensor":"bmp085-1:77","ValueIdx":1}]}
        }],"id":0}
```
   Response:
``` json
    {"id":0,"result":true,"error":null}
```

   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.StrobeMonitor","params":[
        {"UUID":"","Opts":{"Exp_id":2,"Setup_id":2,"Step":1,"Count":1,"Duration":0,"StopAt":"2016-08-16T14:50:00Z",
         "Values":[{"Sensor":"bmp085-1:77","ValueIdx":0},{"Sensor":"bmp085-1:77","ValueIdx":1}],
         "OptsStrict":true}
        }],"id":0}
```
   Response:
``` json
    {"id":0,"result":true,"error":null}
```

7. Lab.GetMonData
   Get monitoring data by UUID and time range.
   May be specified only additionally Start and/or Stop time for range of data.
   Step is used only if storage is support multiplicative Steps storing data, must by specified.
   Returns:
       array  array of objects with
                Time - time in RFC3339 format with TZ and nanoseconds,
                Readings - array of values(ints, floats and etc.) at this Time.
              Empty on error.

   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.GetMonData","params":[
        {"UUID":"b26d1988-b6b5-4b6d-80a6-01bc43e53aab","Start":"2016-08-16T21:22:50.000Z","End":"2016-08-16T21:23:00.000Z","Step":5000000000}],"id":0}
```
   Response:
``` json
    {"id":0,"result":[
        {"Time":"2016-08-16T21:22:59.574Z","Readings":[100139,296.65]},
        {"Time":"2016-08-16T21:23:00.574Z","Readings":[100136,296.65]}
        ],"error":null}
```

   Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.GetMonData","params":[
        {"UUID":"857e2ec6-1099-4879-aa06-0f65a24dad2c","Step":5000000000}],"id":0}
```
   Response:
``` json
    {"id":0,"result":[
        {"Time":"2016-08-17T16:59:29.407Z","Readings":[100741,299.45]},
        {"Time":"2016-08-17T16:59:34.407Z","Readings":[100740,299.45]},
        ...
        {"Time":"2016-08-17T16:59:54.407Z","Readings":[100742,299.34999999999997]},
        {"Time":"2016-08-17T16:59:59.407Z","Readings":[100735,299.34999999999997]}
        ],"error":null}
```


### Methods. Video cameras API

  TODO


### Methods. Time API

  TODO


### Errors examples

  Error request format.
  Request:
``` json
    {}
```
  Response:
``` json
    {"id":null,"result":null,"error":"rpc: service/method request ill-formed: "}
```

  Unknown method error.
  Request:
``` json
    {"jsonrpc":"2.0","method":"Lab.METHOD","params":[],"id":0}
```
  Response:
``` json
    {"id":0,"result":null,"error":"rpc: can't find method Lab.METHOD"}
```
