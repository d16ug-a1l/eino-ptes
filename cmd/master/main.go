package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	openaiModel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino-ptes/pkg/master"
	"github.com/cloudwego/eino-ptes/pkg/remote"
)

func main() {
	var (
		httpAddr    = flag.String("http", ":8080", "http server address")
		tcpAddr     = flag.String("tcp", ":8081", "tcp server address for workers")
		modelAPIKey = flag.String("model-key", os.Getenv("MODEL_API_KEY"), "LLM API key (or env MODEL_API_KEY)")
		modelBaseURL = flag.String("model-url", os.Getenv("MODEL_BASE_URL"), "LLM base URL (or env MODEL_BASE_URL)")
		modelName   = flag.String("model", os.Getenv("MODEL_NAME"), "LLM model name (or env MODEL_NAME)")
	)
	flag.Parse()

	mm := master.NewMemberManager()
	sched := master.NewScheduler(mm)
	gs := master.NewGraphState()

	var chatModel model.BaseChatModel
	if *modelAPIKey != "" && *modelName != "" {
		cfg := &openaiModel.ChatModelConfig{
			APIKey:  *modelAPIKey,
			BaseURL: *modelBaseURL,
			Model:   *modelName,
			Timeout: 30,
		}
		cm, err := openaiModel.NewChatModel(context.Background(), cfg)
		if err != nil {
			log.Fatalf("failed to create chat model: %v", err)
		}
		chatModel = cm
		log.Printf("LLM model enabled: model=%s, baseURL=%s", *modelName, *modelBaseURL)
	} else {
		log.Println("LLM model disabled: set -model-key and -model (or env vars) to enable")
	}

	var analyzer master.ScanAnalyzer
	var planner master.Planner
	if chatModel != nil {
		analyzer = master.NewLLMScanAnalyzer(chatModel)
		planner = master.NewLLMPlanner(chatModel)
	}

	orch := master.NewOrchestrator(sched, mm, gs, analyzer, planner)
	orch.SetToolProvider(func() []tool.BaseTool {
		return remote.RemoteToolSet(sched, mm)
	})
	orch.RefreshToolSet()

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
