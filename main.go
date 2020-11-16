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
	Params struct {
		UID   string `json:"UID"`
		IsOn  bool   `json:"IsOn"`
		Level int    `json:"Level"`
	} `json:"params"`
	ID int `json:"id"`
}

func main() {
	flag.Parse()
	log.SetFlags(0)

	c := connectVoyager(addr)
	defer c.Close()

	quit := make(chan bool)

	go recvFromVoyager(c, quit)
	askForLog(c)
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

				err := c.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("%s\r\n", data)))
				if err != nil {
					log.Println("write:", err)
					return
				}
				log.Printf("send: %s", string(data))
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
	uid := uuid.Must(uuid.NewV4())
	params := fmt.Sprintf("{\"UID\":\"%s\",\"IsOn\":true,\"Level\":0}", uid)
	asklog := fmt.Sprintf("{\"method\":\"RemoteSetLogEvent\",\"params\":%s,\"id\":1}\r\n", params)

	err := c.WriteMessage(websocket.TextMessage, []byte(asklog))
	if err != nil {
		log.Println("write:", err)
		return
	}
	log.Printf("send: %s", asklog)
}
