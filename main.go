package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/gofrs/uuid"
	"github.com/gorilla/websocket"
)

var addr = flag.String("addr", "192.168.0.48:5950", "http service address")

func main() {
	flag.Parse()
	log.SetFlags(0)

	lastpoll := time.Now()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	u := url.URL{Scheme: "ws", Host: *addr, Path: "/"}
	log.Printf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Println("read:", err)
				return
			}
			log.Printf("recv: %s", message)
		}
	}()

	uid := uuid.Must(uuid.NewV4())
	params := fmt.Sprintf("{\"UID\":\"%s\",\"IsOn\":true,\"Level\":0}", uid)
	asklog := fmt.Sprintf("{\"method\":\"RemoteSetLogEvent\",\"params\":%s,\"id\":2}\r\n", params)
	err = c.WriteMessage(websocket.TextMessage, []byte(asklog))
	if err != nil {
		log.Println("write:", err)
		return
	}
	log.Printf("send: %s", asklog)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case t := <-ticker.C:
			now := t
			elapsed := now.Sub(lastpoll)

			if elapsed.Seconds() > 5 {
				lastpoll = now
				secs := now.Unix()

				heartbeat := fmt.Sprintf("{\"Event\":\"Polling\",\"Timestamp\":%d,\"Inst\":1}\r\n", int(secs))
				err := c.WriteMessage(websocket.TextMessage, []byte(heartbeat))
				if err != nil {
					log.Println("write:", err)
					return
				}
				log.Printf("send: %s", heartbeat)
			}

			log.Println("skip")
		case <-interrupt:
			log.Println("interrupt1")

			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("write close:", err)
				return
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}
