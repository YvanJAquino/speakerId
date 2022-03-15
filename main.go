package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/YvanJAquino/speakerId/handlers"
)

func GetEnvDefault(env, def string) string {
	val := os.Getenv(env)
	if val == "" {
		val = def
	}
	return val
}

var (
	parent = context.Background()
	db     = "projects/vocal-etching-343420/instances/speaker-id/database/speaker-id"
	PORT   = GetEnvDefault("PORT", "8081")
)

func main() {
	client, err := spanner.NewClient(parent, db)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	notify, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	handles := &handlers.SpeakerIdHandler{}
	handles.Using(client)

	mux := http.NewServeMux()
	mux.HandleFunc("/get-speaker-ids", handles.GetSpeakerIdsHandler)
	mux.HandleFunc("/register-speaker-ids", handles.RegisterSpeakerIdsHandler)
	mux.HandleFunc("/verify-pin", handles.VerifyPinNumber)

	server := &http.Server{
		Addr:        ":" + PORT,
		Handler:     mux,
		BaseContext: func(net.Listener) context.Context { return parent },
	}
	fmt.Println("Listening and serving on :" + PORT)
	go server.ListenAndServe()
	<-notify.Done()
	fmt.Println("Gracefully shutting down the HTTP/S server")
	shutdown, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	server.Shutdown(shutdown)

}
