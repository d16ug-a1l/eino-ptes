package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloudwego/eino-ptes/pkg/master"
)

func main() {
	var (
		httpAddr = flag.String("http", ":8080", "http server address")
		tcpAddr  = flag.String("tcp", ":8081", "tcp server address for workers")
	)
	flag.Parse()

	mm := master.NewMemberManager()
	sched := master.NewScheduler(mm)
	gs := master.NewGraphState()
	orch := master.NewOrchestrator(sched, mm, gs)

	server := master.NewServer(*httpAddr, *tcpAddr, mm, sched, orch)
	gs.SetServer(server)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("shutting down master...")
		server.Stop()
		cancel()
	}()

	if err := server.Run(ctx); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
