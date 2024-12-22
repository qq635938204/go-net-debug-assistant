package main

import (
	"fmt"
	_ "go-net-debug-assistant/routers"
	"go-net-debug-assistant/service"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/beego/beego/v2/core/logs"
	beego "github.com/beego/beego/v2/server/web"
)

func main() {
	if err := logs.SetLogger(logs.AdapterFile,
		fmt.Sprintf(`{"filename":"%s","daily":true,"level":7}`, "./logs/net-assist.log")); err != nil {
		logs.Error(err)
	}
	logs.EnableFuncCallDepth(true)
	logs.Async()
	if beego.BConfig.RunMode == "dev" {
		beego.BConfig.WebConfig.DirectoryIndex = true
		beego.BConfig.WebConfig.StaticDir["/swagger"] = "swagger"
	}
	service.StartService()
	go beego.Run()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	service.StopService()
	log.Println("End...")
}
