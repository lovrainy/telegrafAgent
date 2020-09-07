# Agent

Agent是用于收集，处理，汇总和编写指标的代理。

设计目标是使插件系统的内存占用最小，以便社区中的开发人员可以轻松添加对收集指标的支持。

Agent是插件驱动的，具有4种不同的插件类型的概念：

1. [Input Plugins](#input-plugins) 从系统，服务或第三方API收集指标
2. [Processor Plugins](#processor-plugins) 转换，修饰和/或过滤指标
3. [Aggregator Plugins](#aggregator-plugins) 创建聚合指标（例如，平均值，最小值，最大值，分位数等）
4. [Output Plugins](#output-plugins) 将指标写入各个目标使用

帮助:

```
agent --help
```

#### 生成一个agent配置文件:

```
agent config > agent.conf
```

#### 仅使用定义的cpu输入和influxdb输出插件生成配置:

```
agent --input-filter cpu --output-filter influxdb config
```

#### 运行单个agent集合，将指标输出到stdout:

```
agent --config telegraf.conf --test
```

#### 使用配置文件中定义的所有插件运行agent:

```
agent --config telegraf.conf
```

#### 运行agent，启用cpu和内存输入，以及influxdb输出插件:

```
agent --config agent.conf --input-filter cpu:mem --output-filter influxdb
```

## Input Plugins

* [cpu](./plugins/inputs/cpu)
* [diskio](./plugins/inputs/diskio)
* [disk](./plugins/inputs/disk)
* [kernel](./plugins/inputs/kernel)
* [logparser](./plugins/inputs/logparser)
* [multiline_logparser](./plugins/inputs/multiline_logparser)
* [memcached](./plugins/inputs/memcached)
* [mem](./plugins/inputs/mem)
* [net](./plugins/inputs/net)
* [netstat](./plugins/inputs/net)
* [system](./plugins/inputs/system)
* [tail](./plugins/inputs/tail)

## Parsers

- [InfluxDB Line Protocol](/plugins/parsers/influx)
- [Collectd](/plugins/parsers/collectd)
- [CSV](/plugins/parsers/csv)
- [Dropwizard](/plugins/parsers/dropwizard)
- [FormUrlencoded](/plugins/parser/form_urlencoded)
- [Graphite](/plugins/parsers/graphite)
- [Grok](/plugins/parsers/grok)
- [JSON](/plugins/parsers/json)
- [Logfmt](/plugins/parsers/logfmt)
- [Nagios](/plugins/parsers/nagios)
- [Value](/plugins/parsers/value), ie: 45 or "booyah"
- [Wavefront](/plugins/parsers/wavefront)

## Serializers

- [InfluxDB Line Protocol](/plugins/serializers/influx)
- [JSON](/plugins/serializers/json)
- [Graphite](/plugins/serializers/graphite)
- [ServiceNow](/plugins/serializers/nowmetric)
- [SplunkMetric](/plugins/serializers/splunkmetric)
- [Carbon2](/plugins/serializers/carbon2)
- [Wavefront](/plugins/serializers/wavefront)

## Processor Plugins

* [converter](./plugins/processors/converter)
* [date](./plugins/processors/date)
* [enum](./plugins/processors/enum)
* [override](./plugins/processors/override)
* [parser](./plugins/processors/parser)
* [pivot](./plugins/processors/pivot)
* [printer](./plugins/processors/printer)
* [regex](./plugins/processors/regex)
* [rename](./plugins/processors/rename)
* [strings](./plugins/processors/strings)
* [tag_limit](./plugins/processors/tag_limit)
* [topk](./plugins/processors/topk)
* [unpivot](./plugins/processors/unpivot)

## Aggregator Plugins

* [basicstats](./plugins/aggregators/basicstats)
* [final](./plugins/aggregators/final)
* [histogram](./plugins/aggregators/histogram)
* [merge](./plugins/aggregators/merge)
* [minmax](./plugins/aggregators/minmax)
* [valuecounter](./plugins/aggregators/valuecounter)

## Output Plugins

* [influxdb](./plugins/outputs/influxdb) (InfluxDB 1.x)
* [influxdb_v2](./plugins/outputs/influxdb_v2) ([InfluxDB 2.x](https://github.com/influxdata/influxdb))
* [cratedb](./plugins/outputs/cratedb)
* [elasticsearch](./plugins/outputs/elasticsearch)
* [exec](./plugins/outputs/exec)
* [file](./plugins/outputs/file)
* [kafka](./plugins/outputs/kafka)
* [opentsdb](./plugins/outputs/opentsdb)
