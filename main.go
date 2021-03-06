package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	_ "time/tzdata"

	Log "github.com/apatters/go-conlog"
	"github.com/gofrs/uuid"
	"github.com/gorilla/websocket"
	ps "github.com/mitchellh/go-ps"
	"github.com/spf13/viper"
)

var (
	addr      = flag.String("addr", "127.0.0.1:5950", "voyager tcp server address")
	logdir    = flag.String("dir", "log", "log directory, default log in program directory")
	verbosity = flag.String("level", "warn", "set log level of clandestine default warn")
)

var (
	heartbeat       event
	counterr        = 0
	cfgFileNotFound = false
)

type loglevel int

const (
	debug loglevel = iota
	info
	warning
	critical
	title
	subtitle
	evnt
	request
	emergency
)

type event struct {
	Event     string  `json:"Event"`
	Timestamp float64 `json:"Timestamp"`
	Host      string  `json:"Host,omitempty"`
	Inst      int     `json:"Inst"`
}

type logevent struct {
	Event     string  `json:"Event"`
	Timestamp float64 `json:"Timestamp"`
	Host      string  `json:"Host"`
	Inst      int     `json:"Inst"`
	TimeInfo  float64 `json:"TimeInfo"`
	Type      int     `json:"Type"`
	Text      string  `json:"Text"`
}

type method struct {
	Method string `json:"method"`
	Params params `json:"params"`
	ID     int    `json:"id"`
}

type params struct {
	UID   string `json:"UID"`
	IsOn  bool   `json:"IsOn"`
	Level *int   `json:"Level,omitempty"`
}

func main() {
	flag.Parse()
	setUpLogs()
	parseConfig()

	c := connectVoyager(addr)
	defer c.Close()

	quit := make(chan bool)
	go recvFromVoyager(c, logdir, quit)
	askForLog(c)
	// remoteSetDashboard(c)
	heartbeatVoyager(c, quit)
}

func parseConfig() {

	viper := readConfig()
	//	launcher()
	if viper == nil {
		fmt.Println("null config")
	}

	if *logdir == "log" && viper.IsSet("voyager.logdir") {
		viperLogdir := viper.GetString("voyager.logdir")
		*logdir = viperLogdir
	}

	var viperVoyAddr string
	var viperVoyPort int

	if viper.IsSet("voyager.tcpserver.address") {
		viperVoyAddr = viper.GetString("voyager.tcpserver.address")
	} else {
		viperVoyAddr = "127.0.0.1"
	}

	if viper.IsSet("voyager.tcpserver.port") {
		viperVoyPort = viper.GetInt("voyager.tcpserver.port")
	} else {
		viperVoyPort = 5950
	}

	if *addr == "127.0.0.1:5950" && (viper.IsSet("voyager.tcpserver.address") || viper.IsSet("voyager.tcpserver.port")) {
		*addr = fmt.Sprintf("%s:%d", viperVoyAddr, viperVoyPort)
	}

	fmt.Println("log dir: ", *logdir)
	fmt.Println("voyager addr: ", *addr)

}

func launcher() {
	// exit if process is already running
	pname := "clandestine"
	if runtime.GOOS == "windows" {
		pname = "clandestine.exe"
	}
	Log.Printf("run %s\n", pname)
	if processAlreadyRunning(pname) {
		fmt.Println("Ok, Clandestine is already running !")
		os.Exit(0)
	}
	fmt.Printf("launching Clandestine ...")
}

func createLogfile(logFilename string, rotate bool) *os.File {
	logfile, err := os.OpenFile(logFilename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		Log.Fatal(err)
	} else {
		if rotate {
			Log.Println("Ok, Rotate Clandestine log")
		} else {
			Log.Println("Ok, Clandestine is launched")
		}
		fmt.Printf("Clandestine log to: %s\n", logFilename)
	}

	return logfile
}

func recvFromVoyager(c *websocket.Conn, logdir *string, quit chan bool) {

	if *logdir == "log" {
		// Switch to default program path
		dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
		if err != nil {
			log.Fatal(err)
		}
		*logdir = fmt.Sprintf("%s/../log", dir)
	}

	logday := currentDateLog()
	logFilename := fmt.Sprintf("%s/%s_Monitor.log", *logdir, logday)
	logdayCurrent := logday
	logfile := createLogfile(logFilename, false)
	defer logfile.Close()
	logger := log.New(logfile, "", log.LstdFlags)

	for {
		select {
		case <-quit:
			return
		default:
			_, message, err := c.ReadMessage()
			if err != nil {
				Log.Println("read:", err)
				logfile.Sync()
				os.Exit(1)
				return
			}

			// manage log file rotation
			logday := currentDateLog()
			if logday != logdayCurrent {
				Log.Printf("We need log a rotation !")
				logfile.Sync()
				logfile.Close()
				logFilename := fmt.Sprintf("%s/%s_Monitor.log", *logdir, logday)
				logdayCurrent = logday
				logfile := createLogfile(logFilename, false)
				defer logfile.Close()
				logger = log.New(logfile, "", log.LstdFlags)
			}

			// parse incoming message
			msg := string(message)
			switch {
			case strings.Contains(msg, `"Event":"ControlData"`):
				Log.Debug("recv controldata")
			case strings.Contains(msg, `"Event":"LogEvent"`):
				ts, level, logline := parseLogEvent(message)
				Log.Debugf("recv log: %.5f %s %s", ts, level, logline)
				logger.Printf("%-9s %s", level, logline)
			case strings.Contains(msg, `"Event":"RemoteActionResult"`):
				Log.Debugf("recv msg: %s", strings.TrimRight(msg, "\r\n"))
			case strings.Contains(msg, `"Event":"Version"`):
				Log.Debugf("recv msg: %s", strings.TrimRight(msg, "\r\n"))
			case strings.Contains(msg, `"Event":"VikingManaged"`):
				Log.Debugf("recv msg: %s", strings.TrimRight(msg, "\r\n"))
			default:
				Log.Debugf("recv not managed: %s", strings.TrimRight(msg, "\r\n"))
			}
		}
	}
}

func processAlreadyRunning(pname string) bool {
	pid := os.Getpid()
	process, _ := ps.Processes()
	for _, p := range process {
		if p.Executable() == pname && p.Pid() != pid {
			fmt.Printf("%s: %d\n", p.Executable(), p.Pid())
			return true
		}
	}
	return false
}

func parseLogEvent(message []byte) (float64, string, string) {

	type logEvent struct {
		Event     string   `json:"Event"`
		Timestamp float64  `json:"Timestamp"`
		Host      string   `json:"Host"`
		Inst      int      `json:"Inst"`
		TimeInfo  float64  `json:"TimeInfo"`
		Type      loglevel `json:"Type"`
		Text      string   `json:"Text"`
	}

	var e logEvent
	err := json.Unmarshal([]byte(message), &e)
	if err != nil {
		Log.Warn("Cannot parse logEvent: %s", err)
	}

	return e.TimeInfo, e.Type.String(), e.Text
}

func (l loglevel) String() string {
	return [...]string{
		"DEBUG",
		"INFO",
		"WARNING",
		"CRITICAL",
		"TITLE",
		"SUBTITLE",
		"EVENT",
		"REQUEST",
		"EMERGENCY",
	}[l-1]
}

func heartbeatVoyager(c *websocket.Conn, quit chan bool) {

	done := make(chan struct{})
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	lastpoll := time.Now()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case t := <-ticker.C:
			now := t
			elapsed := now.Sub(lastpoll)

			// manage heartbeat
			if elapsed.Seconds() > 5 {
				lastpoll = now
				secs := now.Unix()
				heartbeat := &event{
					Event:     "Polling",
					Timestamp: float64(secs),
					Inst:      1,
				}
				data, _ := json.Marshal(heartbeat)
				sendToVoyager(c, data)
			}
		case <-interrupt:
			// Close the read goroutine
			quit <- true
			// Cleanly close the websocket connection by sending a close message
			err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				Log.Println("write close:", err)
				return
			}
			Log.Println("Shutdown clandestine")
			return
		}
	}
}

func connectVoyager(addr *string) *websocket.Conn {
	u := url.URL{Scheme: "ws", Host: *addr, Path: "/"}
	Log.Printf("connecting to %s\n", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		Log.Printf("Can't connect, verify Voyager address or tcp port in the Voyager configuration\n")
		Log.Fatal("Critical: ", err)
	}
	return c
}

func askForLog(c *websocket.Conn) {

	time.Sleep(1 * time.Second)
	level := 0
	p := &params{
		UID:   fmt.Sprintf("%s", uuid.Must(uuid.NewV4())),
		IsOn:  true,
		Level: &level,
	}

	askLog := &method{
		Method: "RemoteSetLogEvent",
		Params: *p,
		ID:     1,
	}

	data, _ := json.Marshal(askLog)
	sendToVoyager(c, data)
}

func remoteSetDashboard(c *websocket.Conn) {

	time.Sleep(2 * time.Second)

	p := &params{
		UID:  fmt.Sprintf("%s", uuid.Must(uuid.NewV4())),
		IsOn: true,
	}

	setDashboard := &method{
		Method: "RemoteSetDashboardMode",
		Params: *p,
		ID:     2,
	}

	data, _ := json.Marshal(setDashboard)
	sendToVoyager(c, data)
}

func sendToVoyager(c *websocket.Conn, data []byte) {

	err := c.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("%s\r\n", data)))
	if err != nil {
		Log.Println("write:", err)
		return
	}
	Log.Debugf("send: %s", data)
}

func currentDateLog() string {
	var d string
	t := time.Now()
	loc, _ := time.LoadLocation("Europe/Paris")

	switch {
	case t.In(loc).Hour() < 12:
		d = fmt.Sprintf("%s", t.AddDate(0, 0, -1).Format("2006-01-02"))
	default:
		d = fmt.Sprintf("%s", t.Format("2006-01-02"))
	}
	return d
}

func setUpLogs() {

	formatter := Log.NewStdFormatter()
	formatter.Options.LogLevelFmt = Log.LogLevelFormatLongTitle
	Log.SetFormatter(formatter)
	switch *verbosity {
	case "debug":
		Log.SetLevel(Log.DebugLevel)
	case "info":
		Log.SetLevel(Log.InfoLevel)
	case "warn":
		Log.SetLevel(Log.WarnLevel)
	case "error":
		Log.SetLevel(Log.ErrorLevel)
	default:
		Log.SetLevel(Log.WarnLevel)
	}

}

func readConfig() *viper.Viper {
	v := viper.New()
	dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatal(err)
	}
	confdir := fmt.Sprintf("%s/conf", dir)
	// if we came from bin directory
	confdir1 := fmt.Sprintf("%s/../conf", dir)
	// Search yaml config file in program path with name "insistent.yaml".
	v.AddConfigPath(confdir)
	v.AddConfigPath(confdir1)
	v.SetConfigType("yaml")
	v.SetConfigName("clandestine")
	//	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			cfgFileNotFound = true
			Log.Debug("Config file not found")
		} else {
			Log.Debug("Something look strange")
			Log.Debugf("error: %v\n", err)
			Log.Debugf("Using config file: %s", v.ConfigFileUsed())
			fmt.Printf("Fatal error something look strange in config file")
			os.Exit(1)
		}
	} else {
		Log.Debugf("Using config file: %s", v.ConfigFileUsed())
	}
	return v
}
