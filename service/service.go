package service

import (
	"encoding/hex"
	"go-net-debug-assistant/communication"

	"github.com/beego/beego/v2/core/logs"
	beego "github.com/beego/beego/v2/server/web"
)

var gTCPServer = &communication.TcpServerEx{}

func StartService() {
	tcpPort := beego.AppConfig.DefaultInt64("tcp_server_port", 10000)
	go gTCPServer.Start(tcpPort, "TCP", tcpMessageHandler)
	udpPort := beego.AppConfig.DefaultInt64("udp_server_port", 9999)
	go communication.StartUDPServer(udpPort)
}

func StopService() {
	logs.Info("Stopping service...")
	defer logs.Info("Service stopped.")
	gTCPServer.Stop()
	communication.StopUDPServer()
}

func tcpMessageHandler(addr string, data []byte) []byte {
	logs.Info("recv from %s, %s", addr, hex.EncodeToString(data))
	return data
}
