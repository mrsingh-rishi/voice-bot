package main

import (
	"fmt"
	"log"
	"net/http"
	"github.com/gorilla/websocket"
)

// Upgrade HTTP connection to WebSocket
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Allow all connections
		return true
	},
}

// WebSocket handler
func handleConnections(w http.ResponseWriter, r *http.Request) {
	// Upgrade the connection
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatalf("WebSocket upgrade failed: %v", err)
	}
	defer ws.Close()

	log.Println("Client Connected")

	for {
		// Read message from client
		// here mesg is of type []byte
		// and msg is of type interface{}
		// we can use type assertion to convert it to string
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Printf("Read error: %v", err)
			break
		}
		log.Printf("Received: %s", msg)
		log.Printf("Type of message: %T", msg)
		// Convert []byte to string
		// and print the type of the string
		// we can use string() function to convert []byte to string
		strMsg := string(msg)
		log.Printf("String message: %s", strMsg)
		log.Printf("Type of string message: %T", strMsg)
		// Echo message back to client
		err = ws.WriteMessage(websocket.TextMessage, []byte(strMsg + "Hello how are you?"))
		if err != nil {
			log.Printf("Write error: %v", err)
			break
		}
	}
}

func main() {
    http.HandleFunc("/ws", handleConnections)
    fmt.Println("Server listening on :9090")
    err := http.ListenAndServe(":9090", nil)
    if err != nil {
        log.Fatalf("ListenAndServe error: %v", err)
    }
}
