package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"time"

	"encoding/json"

	"github.com/gofrs/uuid"
	"github.com/gorilla/websocket"
)

var addr = flag.String("addr", "192.168.0.48:5950", "http service address")
var heartbeat event

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
	log.SetFlags(0)

	c := connectVoyager(addr)
	defer c.Close()

	quit := make(chan bool)
	go recvFromVoyager(c, quit)
	askForLog(c)
	remoteSetDashboard(c)
	heartbeatVoyager(c, quit)

}

func recvFromVoyager(c *websocket.Conn, quit chan bool) {
	for {
		select {
		case <-quit:
			return
		default:
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Println("read:", err)
				return
			}
			log.Printf("recv: %s", message)
		}
	}
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
				writeToWebsocket(c, data)
			}
		case <-interrupt:
			// Close the read goroutine
			quit <- true
			// Cleanly close the websocket connection by sending a close message
			err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("write close:", err)
				return
			}
			fmt.Println("Shutdown clandestine")
			return
		}
	}
}

func connectVoyager(addr *string) *websocket.Conn {
	u := url.URL{Scheme: "ws", Host: *addr, Path: "/"}
	log.Printf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Printf("Can't connect, verify Voyager address or tcp port in the Voyager configuration")
		log.Fatal("Critical: ", err)
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
	writeToWebsocket(c, data)
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
	writeToWebsocket(c, data)
}

func writeToWebsocket(c *websocket.Conn, data []byte) {

	err := c.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("%s\r\n", data)))
	if err != nil {
		log.Println("write:", err)
		return
	}
	log.Printf("send: %s", data)
}
