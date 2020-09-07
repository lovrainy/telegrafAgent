package models

import (
	"reflect"
	"errors"
	"github.com/influxdata/telegraf"
)

// Makemetric applies new metric plugin and agent measurement and tag
// settings.
func makemetric(
	metric telegraf.Metric,
	nameOverride string,
	namePrefix string,
	nameSuffix string,
	tags map[string]string,
	globalTags map[string]string,
) telegraf.Metric {
	if len(nameOverride) != 0 {
		metric.SetName(nameOverride)
	}

	if len(namePrefix) != 0 {
		metric.AddPrefix(namePrefix)
	}
	if len(nameSuffix) != 0 {
		metric.AddSuffix(nameSuffix)
	}

	// Apply plugin-wide tags
	for k, v := range tags {
		if _, ok := metric.GetTag(k); !ok {
			metric.AddTag(k, v)
		}
	}
	// Apply global tags
	// TODO:修改global_tags发送内容
	send_list := []string{"escape_server","daemon","interval","server","agent","kafka_ipport","kafka_topic"}
	for k, v := range globalTags {
		result,_ := Contain(k, send_list)
		if result {
			continue
		}
		if _, ok := metric.GetTag(k); !ok {
			metric.AddTag(k, v)
		}
	}

	return metric
}

func Contain(obj interface{}, target interface{}) (bool, error) {
	targetValue := reflect.ValueOf(target)
	switch reflect.TypeOf(target).Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < targetValue.Len(); i++ {
			if targetValue.Index(i).Interface() == obj {
				return true, nil
			}
		}
	case reflect.Map:
		if targetValue.MapIndex(reflect.ValueOf(obj)).IsValid() {
			return true, nil
		}
	}
	return false, errors.New("not in array")
}
