package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloudwego/eino-ptes/pkg/worker"
)

func main() {
	var (
		id           = flag.String("id", "", "worker unique id")
		name         = flag.String("name", "worker-1", "worker name")
		masterAddr   = flag.String("master", "localhost:8081", "master tcp address")
		capabilities = flag.String("caps", "nmap,nikto,dirb", "comma-separated capabilities")
	)
	flag.Parse()

	if *id == "" {
		*id = generateWorkerID()
	}

	cfg := &worker.Config{
		ID:           *id,
		Name:         *name,
		MasterAddr:   *masterAddr,
		Capabilities: splitCapabilities(*capabilities),
	}

	w := worker.New(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("shutting down worker...")
		w.Stop()
		cancel()
	}()

	if err := w.Run(ctx); err != nil {
		log.Fatalf("worker error: %v", err)
	}
}

func generateWorkerID() string {
	return "worker-" + os.Getenv("HOSTNAME")
}

func splitCapabilities(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}
