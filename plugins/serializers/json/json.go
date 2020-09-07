package json

import (
	"encoding/json"
	"reflect"
	"runtime"
	"time"

	"github.com/influxdata/telegraf"
)

type serializer struct {
	TimestampUnits time.Duration
}

func NewSerializer(timestampUnits time.Duration) (*serializer, error) {
	s := &serializer{
		TimestampUnits: truncateDuration(timestampUnits),
	}
	return s, nil
}

func (s *serializer) Serialize(metric telegraf.Metric) ([]byte, error) {
	m := s.createSingleObject(metric)
	serialized, err := json.Marshal(m)
	if err != nil {
		return []byte{}, err
	}
	serialized = append(serialized, '\n')

	return serialized, nil
}

func (s *serializer) SerializeBatch(metrics []telegraf.Metric) ([]byte, error) {
	objects := make([]interface{}, 0, len(metrics))
	for _, metric := range metrics {
		m := s.createObject(metric)
		objects = append(objects, m)
	}

	obj := map[string]interface{}{
		"metrics": objects,
	}

	serialized, err := json.Marshal(obj)
	if err != nil {
		return []byte{}, err
	}
	return serialized, nil
}

func (s *serializer) createSingleObject(metric telegraf.Metric) map[string]interface{} {
	m := make(map[string]interface{}, 4)
	if metric.Name() == "multiline_logparser" || metric.Name() == "multiline_logparser_win" {
		metric.AddTag("plugin_name", "multiline_logparser")
		// 实际上metric.Fields()内只有一个值，针对日志类型的，field只有msg一个
		m["tags"] = metric.Tags()
		for k, v := range metric.Fields() {
			if k == "msg" {
				m["msg"] = v
			}
		}
	} else {
		// 实际上metric.Fields()内只有一个值
		for k, v := range metric.Fields() {
			m["name"] = k
			if reflect.TypeOf(v).String() == "string" {
				n := make(map[string]interface{})
				n[k] = v
				m["info"] = n
			} else {
				var timestamp int64
				sysType := runtime.GOOS
				if sysType == "linux" {
					timestamp = metric.Time().UnixNano() / int64(s.TimestampUnits)
				}

				if sysType == "windows" {
					timestamp = metric.Time().UnixNano() / 1000000
				}
				m["fields"] = map[string]interface{}{k: v, "timestamp": timestamp}
			}
		}
	}
	tags := make(map[string]string)
	for k, v := range metric.Tags() {
		if k == metric.Name() {
			tags["plugin_name"] = k
		}
		if k == "host" {
			tags["host_name"] = v
		} else {
			tags[k] = v
		}
	}
	m["tags"] = tags

	return m
}

func (s *serializer) createObject(metric telegraf.Metric) map[string]interface{} {
	m := make(map[string]interface{}, 4)
	m["tags"] = metric.Tags()
	m["fields"] = metric.Fields()
	m["name"] = metric.Name()
	m["timestamp"] = metric.Time().UnixNano() / int64(s.TimestampUnits)

	return m
}

func truncateDuration(units time.Duration) time.Duration {
	// Default precision is 1s
	if units <= 0 {
		return time.Second
	}

	// Search for the power of ten less than the duration
	d := time.Nanosecond
	for {
		if d*10 > units {
			return d
		}
		d = d * 10
	}
}
