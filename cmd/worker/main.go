package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudwego/eino-ptes/pkg/worker"
)

func main() {
	var (
		id           = flag.String("id", "", "worker unique id")
		name         = flag.String("name", "worker-1", "worker name")
		masterAddr   = flag.String("master", "localhost:8081", "master tcp address")
		capabilities = flag.String("caps", "nmap,nikto,dirb", "comma-separated capabilities")
		listenAddr   = flag.String("listen", "", "worker listen address for registration info")
	)
	flag.Parse()

	if *id == "" {
		*id = generateWorkerID()
	}
	if *listenAddr == "" {
		*listenAddr = getLocalAddr()
	}

	cfg := &worker.Config{
		ID:           *id,
		Name:         *name,
		MasterAddr:   *masterAddr,
		ListenAddr:   *listenAddr,
		Capabilities: splitCapabilities(*capabilities),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("shutting down worker...")
		cancel()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		w := worker.New(cfg)
		err := w.Run(ctx)
		if err != nil {
			log.Printf("worker error: %v, retrying in 5s...", err)
		} else if ctx.Err() != nil {
			return
		} else {
			log.Println("worker disconnected, retrying in 5s...")
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func generateWorkerID() string {
	return "worker-" + os.Getenv("HOSTNAME")
}

func getLocalAddr() string {
	hostname, err := os.Hostname()
	if err == nil && hostname != "" {
		return hostname
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "localhost"
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "localhost"
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
