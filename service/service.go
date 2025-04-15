package service

import (
	"encoding/hex"
	"go-net-debug-assistant/communication"
	"net"
	"strings"

	"github.com/beego/beego/v2/core/logs"
	beego "github.com/beego/beego/v2/server/web"
)

var gTCPServer = &communication.TcpServerEx{}
var gUDPServer communication.UDPServer

func udpMessageHandler(addr *net.UDPAddr, data []byte) []byte {
	// 处理接收到的UDP消息
	logs.Info("Received UDP message from %s:\n %v\n", addr.String(), data)
	return data
}

func StartService() {
	var sourceIPs []string
	tempSourceIPs := beego.AppConfig.DefaultString("udp_source_ips", "")
	if len(tempSourceIPs) > 0 {
		tmp := strings.Split(tempSourceIPs, ",")
		for _, ip := range tmp {
			tmpIP := strings.TrimSpace(ip)
			if len(tmpIP) > 0 {
				sourceIPs = append(sourceIPs, tmpIP)
			}
		}
	}
	tcpPort := beego.AppConfig.DefaultInt64("tcp_server_port", 10000)
	go gTCPServer.Start(tcpPort, "TCP", tcpMessageHandler)
	udpPort := beego.AppConfig.DefaultInt64("udp_server_port", 9999)
	udpCfg := communication.UDPConfig{
		Port:          int(udpPort),
		BufferSize:    1024,
		ChannelSize:   1024,
		MulticastIP:   beego.AppConfig.DefaultString("udp_multicast_ip", ""),
		InterfaceName: beego.AppConfig.DefaultString("udp_interface_name", ""),
		SourceIPs:     sourceIPs,
	}
	go gUDPServer.Start(udpCfg, udpMessageHandler, false)
}

func StopService() {
	logs.Info("Stopping service...")
	defer logs.Info("Service stopped.")
	gTCPServer.Stop()
	gUDPServer.Stop()
}

func tcpMessageHandler(addr string, data []byte) []byte {
	logs.Info("recv from %s, %s", addr, hex.EncodeToString(data))
	return data
}
