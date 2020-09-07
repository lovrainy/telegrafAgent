package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof" // Comment this line to disable pprof endpoint.
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
	//	"path"
	"path/filepath"
	"bytes"
	//	"plugin"
	"encoding/json"
	"strconv"
	//        "io/ioutil"
	"crypto/cipher"
	"crypto/des"
	//      "encoding/base64"
	"unsafe"
	//        "os/exec"

	"github.com/howeyc/gopass"
	"github.com/influxdata/telegraf/agent"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/internal/config"
	"github.com/influxdata/telegraf/internal/goplugin"
	"github.com/influxdata/telegraf/logger"
	_ "github.com/influxdata/telegraf/plugins/aggregators/all"
	"github.com/influxdata/telegraf/plugins/inputs"
	_ "github.com/influxdata/telegraf/plugins/inputs/all"
	"github.com/influxdata/telegraf/plugins/outputs"
	_ "github.com/influxdata/telegraf/plugins/outputs/all"
	_ "github.com/influxdata/telegraf/plugins/processors/all"
	"github.com/kardianos/service"
	"github.com/influxdata/telegraf/cmd/telegraf/api"
	//psutil "github.com/shirou/gopsutil/process"
)

var fDebug = flag.Bool("debug", false,
	"turn on debug logging")
var pprofAddr = flag.String("pprof-addr", "",
	"pprof address to listen on, not activate pprof if empty")
var fQuiet = flag.Bool("quiet", false,
	"run in quiet mode")
var fTest = flag.Bool("test", false, "enable test mode: gather metrics, print them out, and exit")
var fTestWait = flag.Int("test-wait", 0, "wait up to this many seconds for service inputs to complete in test mode")
var fConfig = flag.String("config", "", "configuration file to load")
var fConfigDirectory = flag.String("config-directory", "",
	"directory containing additional *.conf files")
//var fEditConfig = flag.String("editconfig","","edit config & input config's directory")
//var fEncryptConfig = flag.String("encryptconfig","","encrypt config")
var fVersion = flag.Bool("version", false, "display the version and exit")
var fSampleConfig = flag.Bool("sample-config", false,
	"print out full sample configuration")
var fPidfile = flag.String("pidfile", "", "file to write our pid to")
var fSectionFilters = flag.String("section-filter", "",
	"filter the sections to print, separator is ':'. Valid values are 'agent', 'global_tags', 'outputs', 'processors', 'aggregators' and 'inputs'")
var fInputFilters = flag.String("input-filter", "",
	"filter the inputs to enable, separator is :")
var fInputList = flag.Bool("input-list", false,
	"print available input plugins.")
var fOutputFilters = flag.String("output-filter", "",
	"filter the outputs to enable, separator is :")
var fOutputList = flag.Bool("output-list", false,
	"print available output plugins.")
var fAggregatorFilters = flag.String("aggregator-filter", "",
	"filter the aggregators to enable, separator is :")
var fProcessorFilters = flag.String("processor-filter", "",
	"filter the processors to enable, separator is :")
var fUsage = flag.String("usage", "",
	"print usage for a plugin, ie, 'telegraf --usage mysql'")
var fService = flag.String("service", "",
	"operate on the service (windows only)")
var fServiceName = flag.String("service-name", "telegraf", "service name (windows only)")
var fServiceDisplayName = flag.String("service-display-name", "Telegraf Data Collector Service", "service display name (windows only)")
var fRunAsConsole = flag.Bool("console", false, "run as console application (windows only)")
var fPlugins = flag.String("plugin-directory", "",
	"path to directory containing external plugins")
var fRegister = flag.Bool("register", false, "register agent to server")

var (
	version string
	commit  string
	branch  string
)

var stop chan struct{}

func reloadLoop(
	stop chan struct{},
	inputFilters []string,
	outputFilters []string,
	aggregatorFilters []string,
	processorFilters []string,
) {
	reload := make(chan bool, 1)
	reload <- true
	for <-reload {
		reload <- false

		ctx, cancel := context.WithCancel(context.Background())

		signals := make(chan os.Signal)
		signal.Notify(signals, os.Interrupt, syscall.SIGHUP,
			syscall.SIGTERM, syscall.SIGINT)
		go func() {
			select {
			case sig := <-signals:
				if sig == syscall.SIGHUP {
					log.Printf("I! Reloading Telegraf config")
					<-reload
					reload <- true
				}
				cancel()
			case <-stop:
				cancel()
			}
		}()

		err := runAgent(ctx, inputFilters, outputFilters)
		if err != nil && err != context.Canceled {
			log.Fatalf("E! [telegraf] Error running agent: %v", err)
		}
	}
}

func runAgent(ctx context.Context,
	inputFilters []string,
	outputFilters []string,
) error {
	log.Printf("I! Starting Agent %s", version)

	// If no other options are specified, load the config file and run.
	c := config.NewConfig()
	c.OutputFilters = outputFilters
	c.InputFilters = inputFilters
	err := c.LoadConfig(*fConfig)
	if err != nil {
		//20191009新版telegraf加密迁移
		//if *fRegister {
        //                registpost := c.GetDefault(*fConfig)
        //                err1 := register_agent(registpost)
        //                return err1
        //        }else{
        //                return err
        //        }
		return err
	}
	//defaultdata := make(map[string]string)
    //    defaultdata = c.GetDefault(*fConfig)
	if *fConfigDirectory != "" {
		err = c.LoadDirectory(*fConfigDirectory)
		if err != nil {
			return err
		}
	}
	if !*fTest && len(c.Outputs) == 0 {
		return errors.New("Error: no outputs found, did you provide a valid config file?")
	}
	if *fPlugins == "" && len(c.Inputs) == 0 {
		return errors.New("Error: no inputs found, did you provide a valid config file?")
	}

	if int64(c.Agent.Interval.Duration) <= 0 {
		return fmt.Errorf("Agent interval must be positive, found %s",
			c.Agent.Interval.Duration)
	}

	if int64(c.Agent.FlushInterval.Duration) <= 0 {
		return fmt.Errorf("Agent flush_interval must be positive; found %s",
			c.Agent.Interval.Duration)
	}

	ag, err := agent.NewAgent(c)
	if err != nil {
		return err
	}

	// Setup logging as configured.
	logConfig := logger.LogConfig{
		Debug:               ag.Config.Agent.Debug || *fDebug,
		Quiet:               ag.Config.Agent.Quiet || *fQuiet,
		LogTarget:           ag.Config.Agent.LogTarget,
		Logfile:             ag.Config.Agent.Logfile,
		RotationInterval:    ag.Config.Agent.LogfileRotationInterval,
		RotationMaxSize:     ag.Config.Agent.LogfileRotationMaxSize,
		RotationMaxArchives: ag.Config.Agent.LogfileRotationMaxArchives,
	}

	logger.SetupLogging(logConfig)

	if *fTest || *fTestWait != 0 {
		testWaitDuration := time.Duration(*fTestWait) * time.Second
		return ag.Test(ctx, testWaitDuration)
	}

//      defaultdata := make(map[string]string)
//        defaultdata = c.GetDefault(*fConfig)
//        agentip := defaultdata["agent"]
//        daemon := defaultdata["daemon"]
//        interval := defaultdata["interval"]
//        interval = strings.Replace(interval,"\"","",-1)
//        daemonre,_ := ParseBool(daemon)
//        kafka_ipport := defaultdata["kafka_ipport"]
//        kafka_topic := defaultdata["kafka_topic"]
//        agentip = strings.Replace(agentip,"\"","",-1)
//        //*fPidfile,_ = filepath.Abs(filepath.Dir(os.Args[0]))
//        input_filters := strings.Join(inputFilters,":")
//        output_filters := strings.Join(outputFilters,":")
//        postDir := make(map[string]string)
//	//serverIp := defaultdata["server"]
//        postDir["escape_server"] = defaultdata["escape"]
//        postDir["agent_ip"] = agentip
//        postDir["daemon"] = daemon
//        postDir["interval"] = interval
//        postDir["working_path"] = string(*fPidfile)
//        postDir["input_filter"] = input_filters[1:len(input_filters)-1]
//        postDir["output_filter"] = output_filters[1:len(output_filters)-1]
//        postDir["kafka_ipport"] = defaultdata["kafka_ipport"]
//        postDir["kafka_topic"] = defaultdata["kafka_topic"]
//	process_info, _ := psutil.NewProcess(int32(os.Getpid()))
	//pid_time, _ := process_info.CreateTime()
	//timelayout := "2006-01-02 15:04:05"
	//datetime := time.Unix(int64(pid_time/1000), 0).Format(timelayout)
	//postDir["pid"] =  strconv.Itoa(int(os.Getpid()))
	//postDir["name"],_ = process_info.Name()
	//postDir["work_path"],_ = process_info.Exe()
	//postDir["start_time"] = datetime
	//postDir["command"],_ = process_info.Cmdline()
	//postDir["hostname"],_ = os.Hostname()
	//postDir["name"] = string(postDir["name"])
	//postDir["work_path"] = string(postDir["file"])
	//postDir["start_time"] = string(postDir["start_time"])
	//postDir["command"] = string(postDir["command"])
	//postDir["hostname"] = string(postDir["hostname"])
        // httpPostJson(postDir,"1",serverIp)
        // go httpPostJsonFor(postDir,"1",interval,serverIp)
        //if !daemonre && output_filters[1:len(output_filters)-1] == "kafka" {
        //        testWaitDuration := time.Duration(*fTestWait) * time.Second
        //        return ag.ZeroSecond(ctx, testWaitDuration,kafka_ipport,kafka_topic)
        //}

	log.Printf("I! Loaded inputs: %s", strings.Join(c.InputNames(), " "))
	log.Printf("I! Loaded aggregators: %s", strings.Join(c.AggregatorNames(), " "))
	log.Printf("I! Loaded processors: %s", strings.Join(c.ProcessorNames(), " "))
	log.Printf("I! Loaded outputs: %s", strings.Join(c.OutputNames(), " "))
	log.Printf("I! Tags enabled: %s", c.ListTags())
	//*fPidfile,_ = filepath.Abs(filepath.Dir(os.Args[0]))
    //    pidpath := string(*fPidfile) + "/tele.pid"
    //    f, err := os.OpenFile(pidpath, os.O_CREATE|os.O_WRONLY, 0644)
    //    if err != nil {
    //            log.Printf("E! Unable to create pidfile: %s", err)
    //    } else {
    //            fmt.Fprintf(f, "%d\n", os.Getpid())
	//
    //            f.Close()
	//
    //            defer func() {
    //                    err := os.Remove(*fPidfile)
    //                    if err != nil {
    //                            log.Printf("E! Unable to remove pidfile: %s", err)
    //                    }
    //            }()
    //    }
	if *fPidfile != "" {
		f, err := os.OpenFile(*fPidfile, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("E! Unable to create pidfile: %s", err)
		} else {
			fmt.Fprintf(f, "%d\n", os.Getpid())

			f.Close()

			defer func() {
				err := os.Remove(*fPidfile)
				if err != nil {
					log.Printf("E! Unable to remove pidfile: %s", err)
				}
			}()
		}
	}


	return ag.Run(ctx)
}

func usageExit(rc int) {
	fmt.Println(internal.Usage)
	os.Exit(rc)
}

type program struct {
	inputFilters      []string
	outputFilters     []string
	aggregatorFilters []string
	processorFilters  []string
}

func (p *program) Start(s service.Service) error {
	go p.run()
	return nil
}
func (p *program) run() {
	stop = make(chan struct{})
	reloadLoop(
		stop,
		p.inputFilters,
		p.outputFilters,
		p.aggregatorFilters,
		p.processorFilters,
	)
}
func (p *program) Stop(s service.Service) error {
	close(stop)
	return nil
}

func formatFullVersion() string {
	var parts = []string{"Telegraf"}

	if version != "" {
		parts = append(parts, version)
	} else {
		parts = append(parts, "unknown")
	}

	if branch != "" || commit != "" {
		if branch == "" {
			branch = "unknown"
		}
		if commit == "" {
			commit = "unknown"
		}
		git := fmt.Sprintf("(git: %s %s)", branch, commit)
		parts = append(parts, git)
	}

	return strings.Join(parts, " ")
}

func get_Ip() (string,error) {
        addrs, err := net.InterfaceAddrs()
        if err != nil {
                fmt.Println(err)
                return "",err
        }
        for _, address := range addrs {
                // 检查ip地址判断是否回环地址
                if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
                        if ipnet.IP.To4() != nil {
                                return ipnet.IP.String(),err
                        }
                }
        }
        return "",err
}

func register_agent(registpost map[string]string) error {
        var result, user_name, pass_word string
        var err error
        agentip := registpost["agent"]
        agentip = strings.Replace(agentip,"\"","",-1)
        interval := registpost["interval"]
        interval = strings.Replace(interval,"\"","",-1)
        postDir := make(map[string]string)
        *fPidfile,_ = filepath.Abs(filepath.Dir(os.Args[0]))
	//serverIp := registpost["server"]
        postDir["agentip"] = agentip
        postDir["daemon"] = registpost["daemon"]
        postDir["interval"] = interval
        postDir["escape"] = registpost["escape"]
        postDir["kafka_ipport"] = registpost["kafka_ipport"]
        postDir["kafka_topic"] = registpost["kafka_topic"]
        postDir["input_filters"] = ""
        postDir["output_filters"] = ""
        postDir["pidfile"] = *fPidfile
        //httpPostJson(postDir,"1",serverIp)
        escape,_ := ParseBool(registpost["escape"])
        if escape {
                fmt.Println("此客户端为逃逸客户端，无法注册")
                return err
        }
        fmt.Printf("是否向服务器注册本机？y:注册 u:注销 q:退出")
        fmt.Scanln(&result)
        if result == "y" {
                fmt.Printf("输入主机用户名：")
                fmt.Scanln(&user_name)
                fmt.Printf("输入主机密码：")
                pass, _ := gopass.GetPasswd()
//              fmt.Scanln(&pass_word)
                pass_word = string(pass[:])
                postDir["user_name"] = user_name
                postDir["pass_word"] = pass_word
                postDir["action"] = "register"
                //code := httpPostJson(postDir, "2",serverIp)
                //if code == 200 {
                //        fmt.Println("注册成功")
                //}else{
                //        fmt.Println("注册失败")
                //}
                return err
	}else if result == "u" {
                fmt.Println("注销:")
                fmt.Printf("输入主机用户名：")
                fmt.Scanln(&user_name)
                fmt.Printf("输入主机密码：")
                pass, _ := gopass.GetPasswd()
                pass_word = string(pass[:])
//                fmt.Scanln(&pass_word)
                postDir["user_name"] = user_name
                postDir["pass_word"] = pass_word
                postDir["action"] = "unregister"
                //code := httpPostJson(postDir,"2",serverIp)
                //if code == 200 {
                //        fmt.Println("注销成功")
                //}else{
                //        fmt.Println("注销失败")
                //}
        }else{
                fmt.Println("取消注册")
                return err
        }
        return err

}

func BytesToString(b []byte) string {
 return *(*string)(unsafe.Pointer(&b))
}

func Decrypt(crypted, key []byte, iv []byte) ([]byte, error) {
        block, err := des.NewCipher(key)
        if err != nil {
                return nil, err
        }
        blockMode := cipher.NewCBCDecrypter(block, iv)
        origData := make([]byte, len(crypted))
        // origData := crypted
        blockMode.CryptBlocks(origData, crypted)
        origData = PKCS5UnPadding(origData)
        // origData = ZeroUnPadding(origData)
        return origData, nil
}


func PKCS5UnPadding(origData []byte) []byte {
        length := len(origData)
        unpadding := int(origData[length-1])
        return origData[:(length - unpadding)]
}

func RunServer() {
	go http.ListenAndServe("0.0.0.0:8080", api.MainRouter())

}

func main() {
	flag.Usage = func() { usageExit(0) }
	flag.Parse()
	args := flag.Args()
	RunServer()
//	dir,_ := filepath.Abs(filepath.Dir(os.Args[0]))
//        fmt.Println(dir)
        sectionFilters, inputFilters, outputFilters := []string{}, []string{}, []string{}
//        if *fEncryptConfig != "" {
//		password,_ := ioutil.ReadFile(dir+"/config/.secret")
//		cmd := exec.Command(dir+"/modules/enconfig","-d",*fEncryptConfig,"-p",BytesToString(password))
//                password,_ := ioutil.ReadFile("/home/liuyingtu/gopath/src/github.com/influxdata/telegaf/cmd/telegraf/config/.secret")
//                cmd := exec.Command("/home/liuyingtu/gopath/src/github.com/influxdata/telegaf/cmd/telegraf/modules/enconfig","-d",*fEncryptConfig,"-p",BytesToString(password))
//                cmd.Stdin = os.Stdin
//                cmd.Stdout = os.Stdout
//                err := cmd.Run()
//                if err != nil {
//                        fmt.Println(err)
//                }
//                fmt.Println("encryptconfig")
//                return
//        }
//        if *fEditConfig != "" {
//		fmt.Println(dir+"/modules/runvim")
//		cmd := exec.Command(dir+"/modules/runvim",*fEditConfig)
//                cmd := exec.Command("/home/liuyingtu/gopath/src/github.com/influxdata/telegraf/cmd/telegraf/modules/runvim",*fEditConfig)
//                cmd.Stdin = os.Stdin
//                cmd.Stdout = os.Stdout
//                err := cmd.Run()
//                if err != nil {
//                        fmt.Println("start wrong")
//                }
//                return
//        }

	if *fSectionFilters != "" {
		sectionFilters = strings.Split(":"+strings.TrimSpace(*fSectionFilters)+":", ":")
	}
	if *fInputFilters != "" {
		inputFilters = strings.Split(":"+strings.TrimSpace(*fInputFilters)+":", ":")
	}
	if *fOutputFilters != "" {
		outputFilters = strings.Split(":"+strings.TrimSpace(*fOutputFilters)+":", ":")
	}

	aggregatorFilters, processorFilters := []string{}, []string{}
	if *fAggregatorFilters != "" {
		aggregatorFilters = strings.Split(":"+strings.TrimSpace(*fAggregatorFilters)+":", ":")
	}
	if *fProcessorFilters != "" {
		processorFilters = strings.Split(":"+strings.TrimSpace(*fProcessorFilters)+":", ":")
	}

	logger.SetupLogging(logger.LogConfig{})

	// Load external plugins, if requested.
	if *fPlugins != "" {
		log.Printf("I! Loading external plugins from: %s", *fPlugins)
		if err := goplugin.LoadExternalPlugins(*fPlugins); err != nil {
			log.Fatal("E! " + err.Error())
		}
	}

	if *pprofAddr != "" {
		go func() {
			pprofHostPort := *pprofAddr
			parts := strings.Split(pprofHostPort, ":")
			if len(parts) == 2 && parts[0] == "" {
				pprofHostPort = fmt.Sprintf("localhost:%s", parts[1])
			}
			pprofHostPort = "http://" + pprofHostPort + "/debug/pprof"

			log.Printf("I! Starting pprof HTTP server at: %s", pprofHostPort)

			if err := http.ListenAndServe(*pprofAddr, nil); err != nil {
				log.Fatal("E! " + err.Error())
			}
		}()
	}

	if len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Println(formatFullVersion())
			return
		case "config":
			config.PrintSampleConfig(
				sectionFilters,
				inputFilters,
				outputFilters,
				aggregatorFilters,
				processorFilters,
			)
			return
		}
	}

	// switch for flags which just do something and exit immediately
	switch {
	case *fOutputList:
		fmt.Println("Available Output Plugins:")
		for k := range outputs.Outputs {
			fmt.Printf("  %s\n", k)
		}
		return
	case *fInputList:
		fmt.Println("Available Input Plugins:")
		for k := range inputs.Inputs {
			fmt.Printf("  %s\n", k)
		}
		return
	case *fVersion:
		fmt.Println(formatFullVersion())
		return
	case *fSampleConfig:
		config.PrintSampleConfig(
			sectionFilters,
			inputFilters,
			outputFilters,
			aggregatorFilters,
			processorFilters,
		)
		return
	case *fUsage != "":
		err := config.PrintInputConfig(*fUsage)
		err2 := config.PrintOutputConfig(*fUsage)
		if err != nil && err2 != nil {
			log.Fatalf("E! %s and %s", err, err2)
		}
		return
	}

	shortVersion := version
	if shortVersion == "" {
		shortVersion = "unknown"
	}

	// Configure version
	if err := internal.SetVersion(shortVersion); err != nil {
		log.Println("Telegraf version already configured to: " + internal.Version())
	}

	if runtime.GOOS == "windows" && windowsRunAsService() {
		programFiles := os.Getenv("ProgramFiles")
		if programFiles == "" { // Should never happen
			programFiles = "C:\\Program Files"
		}
		svcConfig := &service.Config{
			Name:        *fServiceName,
			DisplayName: *fServiceDisplayName,
			Description: "Collects data using a series of plugins and publishes it to" +
				"another series of plugins.",
			Arguments: []string{"--config", programFiles + "\\Telegraf\\telegraf.conf"},
		}

		prg := &program{
			inputFilters:      inputFilters,
			outputFilters:     outputFilters,
			aggregatorFilters: aggregatorFilters,
			processorFilters:  processorFilters,
		}
		s, err := service.New(prg, svcConfig)
		if err != nil {
			log.Fatal("E! " + err.Error())
		}
		// Handle the --service flag here to prevent any issues with tooling that
		// may not have an interactive session, e.g. installing from Ansible.
		if *fService != "" {
			if *fConfig != "" {
				svcConfig.Arguments = []string{"--config", *fConfig}
			}
			if *fConfigDirectory != "" {
				svcConfig.Arguments = append(svcConfig.Arguments, "--config-directory", *fConfigDirectory)
			}
			//set servicename to service cmd line, to have a custom name after relaunch as a service
			svcConfig.Arguments = append(svcConfig.Arguments, "--service-name", *fServiceName)

			err := service.Control(s, *fService)
			if err != nil {
				log.Fatal("E! " + err.Error())
			}
			os.Exit(0)
		} else {
			winlogger, err := s.Logger(nil)
			if err == nil {
				//When in service mode, register eventlog target andd setup default logging to eventlog
				logger.RegisterEventLogger(winlogger)
				logger.SetupLogging(logger.LogConfig{LogTarget: logger.LogTargetEventlog})
			}
			err = s.Run()

			if err != nil {
				log.Println("E! " + err.Error())
			}
		}
	} else {
		stop = make(chan struct{})
		reloadLoop(
			stop,
			inputFilters,
			outputFilters,
			aggregatorFilters,
			processorFilters,
		)
	}
}



//func exitByRunDaemon() {
//        *fPidfile,_ = filepath.Abs(filepath.Dir(os.Args[0]))
//        f,_ := os.Open(string(*fPidfile) + "/tele.pid")
//        pid,_ := ioutil.ReadAll(f)
//        pidre,_ := strconv.Atoi(string(pid))
//        syscall.Kill(pidre, syscall.SIGKILL)
//}


type Cmd struct {
        agent_ip string
}

func ParseBool(str string) (bool, error) {
    switch str {
    case "1", "t", "T", "true", "TRUE", "True":
        return true, nil
    case "0", "f", "F", "false", "FALSE", "False":
        return false, nil
    }
    return false, nil
}

func strip(s_ string, chars_ string) string {
        s , chars := []rune(s_) , []rune(chars_)
        length := len(s)
        max := len(s) - 1
        l, r := true, true //标记当左端或者右端找到正常字符后就停止继续寻找
        start, end := 0, max
        tmpEnd := 0
        charset := make(map[rune]bool) //创建字符集，也就是唯一的字符，方便后面判断是否存在
        for i := 0; i < len(chars); i++ {
                charset[chars[i]] = true
        }
        for i := 0; i < length; i++ {
                if _, exist := charset[s[i]]; l && !exist {
                        start = i
                        l = false
                }
                tmpEnd = max - i
                if _, exist := charset[s[tmpEnd]]; r && !exist {
                        end = tmpEnd
                        r = false
                }
                if !l && !r{
                        break
                }
        }
        if l && r {  // 如果左端和右端都没找到正常字符，那么表示该字符串没有正常字符
                return ""
        }
        return string(s[start : end+1])
}

func httpPostJsonFor(postDir map[string]string, postType string, interval string, serverIp string) {
	inter,_ := strconv.Atoi(interval)
	//fmt.Println(inter)
	for {
                httpPostJson(postDir,postType,serverIp)
                time.Sleep(time.Second*time.Duration(inter))
        }
}

func httpPostJson(postDir map[string]string, postType string, serverIp string) int {
        var url string
//	fmt.Println(postDir)
        postDir["kafka_ipport"] = strip(postDir["kafka_ipport"],"\"")
        postDir["kafka_topic"] = strip(postDir["kafka_topic"],"\"")
        //contentType := "application/json;charset=utf-8"
        cmd := Cmd{agent_ip : postDir["agent_ip"]}
        b ,err := json.Marshal(cmd)
        if err != nil {
                log.Println("json format error:", err)
                return 1
        }
        body := bytes.NewBuffer(b)
//        var dir_str string
	var dir string
	postDir["secret"] = "E t i c k e t M o n i t"
        if postType == "1" {
                url = "http://"+serverIp+"/server/agent/escape"
//                dir_str = "{\"agent_ip\":\""+postDir["agentip"]+"\",\"escape_server\":\""+postDir["escape"]+"\",\"daemon\":\""+postDir["daemon"]+"\",\"interval\":\""+postDir["interval"]+"\",\"working_path\":\""+postDir["pidfile"]+"\",\"input_filter\":\""+postDir["input_filters"]+"\",\"output_filter\":\""+postDir["output_filters"]+"\",\"pid\":\""+postDir["pid"]+"\",\"name\":\""+postDir["name"]+"\",\"work_path\":\""+postDir["file"]+"\",\"start_time\":\""+postDir["start_time"]+"\",\"command\":\""+postDir["command"]+"\",\"hostname\":\""+postDir["hostname"]+"\",\"kafka_ipport\":\""+kafka_ipport+"\",\"kafka_topic\":\""+kafka_topic+"\",\"secret\":\"E t i c k e t M o n i t\"}"
//		dir_str = strings.TrimSpace(dir_str)
		dir_json,_ := json.Marshal(postDir)
		dir = BytesToString(dir_json)
        }else{
                url = "http://"+serverIp+"/server/agent/control"
//                dir_str = "{\"address\":\""+postDir["agentip"]+"\",\"escape_server\":\""+postDir["escape"]+"\",\"daemon\":\""+postDir["daemon"]+"\",\"interval\":\""+postDir["interval"]+"\",\"working_path\":\""+postDir["pidfile"]+"\",\"kafka_ipport\":\""+kafka_ipport+"\",\"kafka_topic\":\""+kafka_topic+"\",\"user\":\""+postDir["user_name"]+"\",\"password\":\""+postDir["pass_word"]+"\",\"action\":\""+postDir["action"]+"\",\"secret\":\"E t i c k e t M o n i t\"}"
//                dir_str = strings.TrimSpace(dir_str)
		dir_json,_ := json.Marshal(postDir)
		dir = BytesToString(dir_json)
        }
        url = strings.Replace(url,"\"","",-1)
        client := &http.Client{Timeout: time.Duration(3 * time.Second)}
        req,err := http.NewRequest("POST",url,body)
//      fmt.Println(dir)
        str1 := "DESCRYPT"
        iv1 := "        "
        data := []byte(dir)
        key := []byte(str1)
        iv := []byte(iv1)
        a,_ := Encrypt(data,key,iv)
        var restr string
        for i := 0; i<len(a);i++ {
                restr += strconv.Itoa(int(a[i])) +","
        }
	req.Header.Add("token",restr[:len(restr)-1])
        resp, err := client.Do(req)
        if err != nil {
                log.Println("Post failed:", err)
                return 1
        }
        code := resp.StatusCode
        defer resp.Body.Close()
        return code
}

func Encrypt(origData, key []byte, iv []byte) ([]byte, error) {
        block, err := des.NewCipher(key)
        if err != nil {
                return nil, err
        }
        origData = PKCS5Padding(origData, block.BlockSize())
        // origData = ZeroPadding(origData, block.BlockSize())
        blockMode := cipher.NewCBCEncrypter(block, iv)
        crypted := make([]byte, len(origData))
        // 根据CryptBlocks方法的说明，如下方式初始化crypted也可以
        // crypted := origData
        blockMode.CryptBlocks(crypted, origData)
        return crypted, nil
}

func PKCS5Padding(ciphertext []byte, blockSize int) []byte {
        padding := blockSize - len(ciphertext)%blockSize
        padtext := bytes.Repeat([]byte{byte(padding)}, padding)
        return append(ciphertext, padtext...)
}
// Return true if Telegraf should create a Windows service.
func windowsRunAsService() bool {
	if *fService != "" {
		return true
	}

	if *fRunAsConsole {
		return false
	}

	return !service.Interactive()
}
