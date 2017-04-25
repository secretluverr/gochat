

package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"fmt"
	"strings"
	"encoding/json"
)

const (
// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	hub *Hub

	// The websocket connection.
	conn *websocket.Conn
	name string
	// Buffered channel of outbound messages.
	send chan []byte
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.

type msg struct {
	User 			string 			`json:"user"`
	Text	 		string			`json:"text"`
}

var is_unicast bool
var is_blockcast bool
var is_new_name bool
var is_gucci bool

var sender string
var recipient string
var gucci string

func irccall(m *msg, c *Client) {
	/*
	Expected data formats

	'message'                         --Broadcast text 'message, default case
	/msg 'username' 'msg'             --Unicast text 'message' to 'username'
	/bmsg 'username' 'msg'            --Blockcast text, send to all except 'username'
	/bcfile 'filepath'                --Broadcast file to all users, where 'filepath' is
			full path on localmachine
	/ucfile 'username' 'filepath'     --Unicast file to 'username', where 'filepath' is
			full path on localmachine
	/nick 'username'                  --Create/reassign new user.
	/gucci                            --Does something interesting*/

	partition := strings.Fields(m.Text)
	fmt.Println("PARTITION: ",partition)
	for i := 0;i<len(partition);i++ {
		fmt.Println(i, " : ",partition[i])
	}
	if strings.Compare(partition[0],"/msg") == 0 {
		is_unicast = true
		is_blockcast = false
		is_new_name = false
		is_gucci = false

		sender = m.User
		if(len(partition) < 2) {
			return;
		}
		recipient = partition[1]

	} else if (strings.Compare(partition[0],"/bmsg")) == 0 {
		is_unicast = false
		is_blockcast = true
		is_new_name = false
		is_gucci = false
		if(len(partition) < 2) {
			return;
		}
		sender = m.User
		recipient = partition[1]
	} else if (strings.Compare(partition[0],"/nick")) == 0 {
		is_unicast = false
		is_blockcast = false
		is_new_name = true
		is_gucci = false

		sender = m.User
		if(len(partition) < 2) {
			return;
		}
		m.User = partition[1]
		c.name = partition[1]
		fmt.Println("NEW NICKNAME: ",m.User)
	} else if (strings.Compare(partition[0],"/gucci")) == 0 {
		is_unicast = false
		is_blockcast = false
		is_new_name = false
		is_gucci = true


		m.Text = gucci
	}

	return
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		m := &msg{User:c.name, Text:string(message)}

		if message[0] == '/' {
			irccall(m,c)
		}
		str_message, err := json.Marshal(m)

		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				log.Printf("error: %v", err)
			}
			break
		}
		message = str_message
		fmt.Println(string(message))
		c.hub.broadcast <- message
	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)

			if err != nil {
				return
			}

			w.Write(message)

		// Add queued chat messages to the current websocket message.
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write(newline)
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

// serveWs handles websocket requests from the peer.
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256)}
	client.name = client.conn.RemoteAddr().String()
	client.hub.register <- client
	go client.writePump()
	client.readPump()
}
