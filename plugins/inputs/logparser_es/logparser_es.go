// +build !solaris

package logparser_es

import (
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
//	"sync/atomic"
//	"github.com/koangel/grapeTimer"

	"github.com/influxdata/tail"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal/globpath"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/parsers"
)

const (
	defaultWatchMethod = "inotify"
)

var (
	offsets      = make(map[string]int64)
	offsetsMutex = new(sync.Mutex)
)

// LogParser in the primary interface for the plugin
type GrokConfig struct {
	MeasurementName    string `toml:"measurement"`
	Patterns           []string
	NamedPatterns      []string
	CustomPatterns     string
	CustomPatternFiles []string
	Timezone           string
	UniqueTimestamp    string
}

type logEntry struct {
	path string
	line string
}
// 20191006: 记录log文件inode与offset。
type FileWatch struct {
	name string  // log文件名称
	watchfile string // 缓存文件名称
	inode uint64 // log文件的inode
	offset int64  // seek位移
	watchmd5 string // n行内容的md5记录
}
var filewatchMap = make(map[string]*FileWatch)

// LogParserPlugin is the primary struct to implement the interface for logparser plugin
type LogParserPlugin struct {
	Files         []string
	FromBeginning bool
	// 20191006增加：缓存记录文件目录
	Watchdir      string
	WatchMethod   string
	Switch        bool
	Tail_interval float64
	Log           telegraf.Logger
	Timeout       int
	Multi_enable  bool

	tailers map[string]*tail.Tail
	offsets map[string]int64
	lines   chan logEntry
	done    chan struct{}
	wg      sync.WaitGroup
	acc     telegraf.Accumulator

	sync.Mutex

	GrokParser parsers.Parser
	GrokConfig GrokConfig `toml:"grok"`
}

func NewLogParser() *LogParserPlugin {
	offsetsMutex.Lock()
	offsetsCopy := make(map[string]int64, len(offsets))
	for k, v := range offsets {
		offsetsCopy[k] = v
	}
	offsetsMutex.Unlock()

	return &LogParserPlugin{
		WatchMethod: defaultWatchMethod,
		offsets:     offsetsCopy,
	}
}

const sampleConfig = `
  ## Log files to parse.
  ## These accept standard unix glob matching rules, but with the addition of
  ## ** as a "super asterisk". ie:
  ##   /var/log/**.log     -> recursively find all .log files in /var/log
  ##   /var/log/*/*.log    -> find all .log files with a parent dir in /var/log
  ##   /var/log/apache.log -> only tail the apache log file
  files = ["/var/log/apache/access.log"]

  ## Read files that currently exist from the beginning. Files that are created
  ## while telegraf is running (and that match the "files" globs) will always
  ## be read from the beginning.
  from_beginning = false

  ## Method used to watch for file updates.  Can be either "inotify" or "poll".
  # watch_method = "inotify"

  ## Parse logstash-style "grok" patterns:
  [inputs.logparser.grok]
    ## This is a list of patterns to check the given log file(s) for.
    ## Note that adding patterns here increases processing time. The most
    ## efficient configuration is to have one pattern per logparser.
    ## Other common built-in patterns are:
    ##   %{COMMON_LOG_FORMAT}   (plain apache & nginx access logs)
    ##   %{COMBINED_LOG_FORMAT} (access logs + referrer & agent)
    patterns = ["%{COMBINED_LOG_FORMAT}"]

    ## Name of the outputted measurement name.
    measurement = "apache_access_log"

    ## Full path(s) to custom pattern files.
    custom_pattern_files = []

    ## Custom patterns can also be defined here. Put one pattern per line.
    custom_patterns = '''
    '''

    ## Timezone allows you to provide an override for timestamps that
    ## don't already include an offset
    ## e.g. 04/06/2016 12:41:45 data one two 5.43µs
    ##
    ## Default: "" which renders UTC
    ## Options are as follows:
    ##   1. Local             -- interpret based on machine localtime
    ##   2. "Canada/Eastern"  -- Unix TZ values like those found in https://en.wikipedia.org/wiki/List_of_tz_database_time_zones
    ##   3. UTC               -- or blank/unspecified, will return timestamp in UTC
    # timezone = "Canada/Eastern"

	## When set to "disable", timestamp will not incremented if there is a
	## duplicate.
    # unique_timestamp = "auto"
`


// SampleConfig returns the sample configuration for the plugin
func (l *LogParserPlugin) SampleConfig() string {
	return sampleConfig
}

// Description returns the human readable description for the plugin
func (l *LogParserPlugin) Description() string {
	return "Stream and parse log file(s)."
}

// Gather is the primary function to collect the metrics for the plugin
func (l *LogParserPlugin) Gather(acc telegraf.Accumulator) error {
	l.Lock()
	defer l.Unlock()

	// always start from the beginning of files that appear while we're running
	return l.tailNewfiles(true)
}

// 流程： Start ---> tailNewfiles ---> receiver(此处做行数与md5记录)
// 20191006: 生成md5方法
func md5Str(str string) string {
	w := md5.New()
	io.WriteString(w, str)
	md5str := fmt.Sprintf("%x", w.Sum(nil))
	return md5str
}

// 20191009更新：文件正则查找（目前支持单层(*.)后缀匹配，嵌套(**.后缀)）
func findfile(root string) []string {
	rootDir := filepath.Dir(root)
	exprOld := filepath.Base(root)

	var expr string

	var fileList []string
	if exprOld[:1] == "*" || exprOld[:2] == "**"{
		if exprOld[:1] == "*" {
			expr = ".*" + "\\" + strings.Split(exprOld, "*")[1]
		}
		if exprOld[:2] == "**" {
			expr = ".*" + "\\" + strings.Split(exprOld, "**")[1]
		}
		regex, err := regexp.Compile(expr)
		if err != nil {
			fmt.Println("Invalid regular expression.")
			os.Exit(1)
		}
		_ = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
			}
			if regex.MatchString(info.Name()) {
				//fmt.Println(filepath.Dir(path))
				if filepath.Dir(path) == rootDir &&  exprOld[:1] == "*" {
					fileList = append(fileList, path)
				} else if filepath.Dir(path) == rootDir &&  exprOld[:2] == "**" {
					fileList = append(fileList, path)
				}
		}
			return nil
		})
	} else {
		fileList = append(fileList, root)
	}
	return fileList
}

// Start kicks off collection of stats for the plugin
func (l *LogParserPlugin) Start(acc telegraf.Accumulator) error {
	l.Lock()
	defer l.Unlock()
	// 第一步：判断缓存文件是否存在, 不存在即创建
	watchdir := l.Watchdir
	_, watcherr := os.Stat(watchdir)
	if os.IsNotExist(watcherr) {
		watcherr = os.Mkdir(watchdir, os.FileMode(0755))
		if watcherr != nil {
			// TODO: 正式版移除panic
			panic(watcherr)
		}
	}

	// 修正正则匹配的文件列表
	var fixFileList []string
	for _, filename := range l.Files {
		for _, newFile := range findfile(filename) {
			fixFileList = append(fixFileList, newFile)
		}
	}
	l.Files = fixFileList

	// 第二步：判断各文件的缓存记录文件是否存在，不存在即创建，且在其中初始化记录log文件的（inode，seek）。seek初始化为0.
	//        存在即读取inode与seek记录。以便后续读取时做对比。
	for _, filename := range l.Files {
		filewatch := FileWatch{name: filename}
		// 初始化当前文件缓存记录文件地址
		filewatch.watchfile = path.Join(watchdir,  path.Base(filename))

		// 文件不存在，则panic退出
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			panic("filename: " + filename + " does not exist!")
		} else {
			// 获取当前文件inode
			var filestat syscall.Stat_t
			if err := syscall.Stat(filename, &filestat); err != nil {
				panic(err)
			}
			filewatch.inode = filestat.Ino
			// 初始化md5
			filewatch.watchmd5 = md5Str(filename + strconv.FormatUint(filewatch.inode, 10))
		}

		_, err := os.Stat(filewatch.watchfile)
		if os.IsNotExist(err) {
			// 缓存文件不存在，则创建
			f, err := os.Create(filewatch.watchfile)
			f.Close()
			if err != nil {
				panic(err)
			} else {
				// 初始化offset
				filewatch.offset = 0
			}
		} else {
			// 缓存文件存在则读取信息
			b, err := ioutil.ReadFile(filewatch.watchfile)
			if err != nil {
				panic(err)
			} else {
				bString := string(b)
				// 缓存文件非空才做读取处理
				if bString != "" {
					// 获取上次记录的offset信息
					bSplit := strings.Split(bString, "\n")
					filewatch.inode, _ = strconv.ParseUint(bSplit[0], 10, 64)
					filewatch.offset, _ = strconv.ParseInt(bSplit[1], 10, 64)
					// 获取上次记录的md5
					oldwatchmd5 := bSplit[2]
					// 对上次记录的md5与本次的md5做对比，不同则清空文件内容,offset初始化为0
					if oldwatchmd5 != filewatch.watchmd5 {
						filewatch.offset = 0
						f, _ := os.OpenFile(filewatch.name, os.O_WRONLY| os.O_TRUNC, 0600)
						f.Close()
					}
				}
			}
		}
		// 至此，首次打开：已记录filename、inode、watchfile、行数归零
		// 重新打开：已记录filename、上次记录的inode、watchfile、上次记录的行数、上次记录的n行内容的md5
		filewatchMap[filename] = &filewatch
	}

	l.acc = acc
	l.lines = make(chan logEntry, 1000)
	l.done = make(chan struct{})
	l.tailers = make(map[string]*tail.Tail)

	mName := "logparser_es"
	if l.GrokConfig.MeasurementName != "" {
		mName = l.GrokConfig.MeasurementName
	}

	// Looks for fields which implement LogParser interface
	config := &parsers.Config{
		MetricName:             mName,
		GrokPatterns:           l.GrokConfig.Patterns,
		GrokNamedPatterns:      l.GrokConfig.NamedPatterns,
		GrokCustomPatterns:     l.GrokConfig.CustomPatterns,
		GrokCustomPatternFiles: l.GrokConfig.CustomPatternFiles,
		GrokTimezone:           l.GrokConfig.Timezone,
		GrokUniqueTimestamp:    l.GrokConfig.UniqueTimestamp,
		DataFormat:             "grok",
	}

	var err error
	l.GrokParser, err = parsers.NewParser(config)
	if err != nil {
		return err
	}

	fileLine = make(map[string]*fileLineIns)
	for _, file := range l.Files {
		fileLine[file] = &fileLineIns{
			last:"",
			curtime:time.Now(),
		}
	}
	go l.checkTime()

	l.wg.Add(1)
	//判断是否开启多行模式
	if l.Multi_enable {
		go l.multilineParser()
	}else{
		go l.parser()
	}


	err = l.tailNewfiles(l.FromBeginning)

	// clear offsets
	l.offsets = make(map[string]int64)
	// assumption that once Start is called, all parallel plugins have already been initialized
	offsetsMutex.Lock()
	offsets = make(map[string]int64)
	offsetsMutex.Unlock()

	return err
}

func (l *LogParserPlugin) checkTime() {
	for {
		for file, v := range fileLine {
			cur := time.Now()
			timeSub := cur.Sub(v.curtime)
			if timeSub > time.Duration(l.Timeout)*time.Second {
				if fileLine[file].last != "" {
					fmt.Println("已超时")
					var m telegraf.Metric
					m, _ = l.GrokParser.ParseLine(fileLine[file].last)

					m.AddField("msg", fileLine[file].last)
					tags := m.Tags()
					tags["path"] = file
					// send msg
					l.acc.AddFields(m.Name(), m.Fields(), tags, m.Time())
					fileLine[file].last = ""
					fileLine[file].curtime = time.Now()
				}
			}
		}
	}

}

// check the globs against files on disk, and start tailing any new files.
// Assumes l's lock is held!
func (l *LogParserPlugin) tailNewfiles(fromBeginning bool) error {
	var poll bool
	if l.WatchMethod == "poll" {
		poll = true
	}

	// Create a "tailer" for each file
	for _, filepath := range l.Files {
		g, err := globpath.Compile(filepath)
		if err != nil {
			l.Log.Errorf("Glob %q failed to compile: %s", filepath, err)
			continue
		}
		files := g.Match()

		for _, file := range files {
			if _, ok := l.tailers[file]; ok {
				// we're already tailing this file
				continue
			}

			var seek *tail.SeekInfo
			if !fromBeginning {
				// 20191006：修正此处获取文件offset
				seek = &tail.SeekInfo{
					Whence: io.SeekStart,
					Offset: filewatchMap[file].offset,
				}
				} else {
					// 从log开头开始tail
					seek = &tail.SeekInfo{
							Whence: io.SeekStart,
							Offset: 0,
				}
			}

			tailer, err := tail.TailFile(file,
				tail.Config{
					ReOpen:    true,
					Follow:    true,
					Location:  seek,
					MustExist: true,
					Poll:      poll,
					Logger:    tail.DiscardingLogger,
				})
			if err != nil {
				l.acc.AddError(err)
				continue
			}

			l.Log.Debugf("Tail added for file: %v", file)

			// create a goroutine for each "tailer"
			l.wg.Add(1)
			go l.receiver(tailer)
//			go l.to_Stop()
			l.tailers[file] = tailer
		}
	}

	return nil
}


func (l *LogParserPlugin)to_Stop() {
//	var wg sync.WaitGroup
//	var m telegraf.Metric
//	wg.Add(1)
//	ticker := time.NewTicker(time.Second * 5)
//	go func() {
//		defer wg.Done()
//		for {
//			<-ticker.C
//			count := make(map[string]interface{})
  //                      tags := make(map[string]string)
    //                    count["total_count"] = uint64Format(l.Line_count)
//                        tags["total_files"] = strings.Join(l.Files,",")
//                        l.acc.AddFields("logparser",count,tags,time.Now())
//                        l.Line_count = 0
//		}
//	}()
//	Id = grapeTimer.NewTickerLoop(10000, -1, send_LineCount)
}


// receiver is launched as a goroutine to continuously watch a tailed logfile
// for changes and send any log lines down the l.lines channel.
func (l *LogParserPlugin) receiver(tailer *tail.Tail) {
	defer l.wg.Done()
	var line *tail.Line
	for line = range tailer.Lines {
		// 20191006: 每次都重新记录log的offset
		offset, _ := tailer.Tell()
		filewatchMap[tailer.Filename].offset = offset
		// 每次都重新校验md5，且记录到缓存文件。
		var filestat syscall.Stat_t
		if err := syscall.Stat(tailer.Filename, &filestat); err != nil {
			panic(err)
		}
		inode := filestat.Ino
		filewatchMap[tailer.Filename].inode = filestat.Ino
		filewatchMap[tailer.Filename].watchmd5 = md5Str(tailer.Filename + strconv.FormatUint(inode, 10))
		// 重新记录到文件
		f, err1 := os.OpenFile(filewatchMap[tailer.Filename].watchfile, os.O_WRONLY | os.O_TRUNC, 0644)
		if err1 != nil {
			panic(err1)
		}
		content := strconv.FormatUint(inode, 10) + "\n" + strconv.FormatInt(offset, 10) + "\n" + filewatchMap[tailer.Filename].watchmd5
		_, err := f.WriteString(content)
		if err != nil {
			panic(err)
		}
		f.Close()

		// 原代码
		if line.Err != nil {
			l.Log.Errorf("Error tailing file %s, Error: %s",
				tailer.Filename, line.Err)
			continue
		}

		// Fix up files with Windows line endings.
		text := strings.TrimRight(line.Text, "\r")

		entry := logEntry{
			path: tailer.Filename,
			line: text,
		}

		select {
		case <-l.done:
		case l.lines <- entry:
		}
	}
}

func uint64Format(metric uint64) float64 {
        metricStr := strconv.FormatUint(metric, 10)
        metricFloat, _ := strconv.ParseFloat(metricStr,64)
        return metricFloat
}

type fileLineIns struct {
	last string
	curtime time.Time
}

var fileLine map[string]*fileLineIns

// parse is launched as a goroutine to watch the l.lines channel.
// when a line is available, parse parses it and adds the metric(s) to the
// accumulator.
func (l *LogParserPlugin) parser() {
	defer l.wg.Done()

	var m telegraf.Metric
	var err error
	var entry logEntry
	for {
		select {
		case <-l.done:
			return
		case entry = <-l.lines:
			if entry.line == "" || entry.line == "\n" {
				continue
			}
		}
		m, err = l.GrokParser.ParseLine(entry.line)
		if err == nil {
			if m != nil {
				tags := m.Tags()
				tags["path"] = entry.path
				// send msg
				l.acc.AddFields(m.Name(), m.Fields(), tags, m.Time())
				time.Sleep(time.Duration(l.Tail_interval)*time.Second)
			}
		} else {
			l.Log.Errorf("Error parsing log line: %s", err.Error())
		}

	}
}

func (l *LogParserPlugin) multilineParser() {
	defer l.wg.Done()

	var m telegraf.Metric
	var err error
	var entry logEntry
	for {
		select {
		case <-l.done:
			return
		case entry = <-l.lines:
			if entry.line == "" || entry.line == "\n" {
				continue
			}
		}
		m, err = l.GrokParser.ParseLine(entry.line)
		if err == nil {
			if m != nil {
				if len(m.Fields()) == 0 {
					fileLine[entry.path].last += entry.line + "\n"
					fileLine[entry.path].curtime = time.Now()
				} else {
					if fileLine[entry.path].last == "" {
						fileLine[entry.path].last += entry.line + "\n"
						fileLine[entry.path].curtime = time.Now()
					} else {
						m.AddField("msg", fileLine[entry.path].last)
						tags := m.Tags()
						tags["path"] = entry.path
						// send msg
						l.acc.AddFields(m.Name(), m.Fields(), tags, m.Time())
						fileLine[entry.path].last = entry.line + "\n"
						fileLine[entry.path].curtime = time.Now()
					}
				}
			} else {
				fileLine[entry.path].last += entry.line + "\n"
				fileLine[entry.path].curtime = time.Now()
			}
		} else {
			l.Log.Errorf("Error parsing log line: %s", err.Error())
		}

	}
}

// Stop will end the metrics collection process on file tailers
func (l *LogParserPlugin) Stop() {
	l.Lock()
	defer l.Unlock()

	for _, t := range l.tailers {
		if !l.FromBeginning {
			// store offset for resume
			offset, err := t.Tell()
			if err == nil {
				l.offsets[t.Filename] = offset
				l.Log.Debugf("Recording offset %d for file: %v", offset, t.Filename)
			} else {
				l.acc.AddError(fmt.Errorf("error recording offset for file %s", t.Filename))
			}
		}
		err := t.Stop()

		//message for a stopped tailer
		l.Log.Debugf("Tail dropped for file: %v", t.Filename)

		if err != nil {
			l.Log.Errorf("Error stopping tail on file %s", t.Filename)
		}
	}
	close(l.done)
	l.wg.Wait()

	// persist offsets
	offsetsMutex.Lock()
	for k, v := range l.offsets {
		offsets[k] = v
	}
	offsetsMutex.Unlock()
}

func init() {
	inputs.Add("logparser_es", func() telegraf.Input {
		return NewLogParser()
	})
}
