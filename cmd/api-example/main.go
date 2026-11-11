package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jonathanselby/api-interface/internal/exampleapi"
	"github.com/jonathanselby/api-interface/internal/exampleapp"
)

func main() {
	address := flag.String("listen", "127.0.0.1:8080", "HTTP bind address")
	cert := flag.String("tls-cert", "", "TLS certificate (optional)")
	key := flag.String("tls-key", "", "TLS private key (optional)")
	openapi := flag.Bool("openapi", false, "print OpenAPI and exit")
	flag.Parse()
	if (*cert == "") != (*key == "") {
		log.Fatal("TLS cert and key must be supplied together")
	}
	example, err := exampleapi.New(exampleapp.NewStore(), nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	if *openapi {
		fmt.Println(string(example.Handler.OpenAPI()))
		return
	}
	server := &http.Server{
		Addr: *address, Handler: example.Handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}()
	log.Printf("API Interface example listening on %s", *address)
	if *cert != "" {
		err = server.ListenAndServeTLS(*cert, *key)
	} else {
		err = server.ListenAndServe()
	}
	if err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
