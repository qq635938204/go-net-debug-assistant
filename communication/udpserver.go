package communication

import (
	"encoding/hex"
	"fmt"
	"net"
	"sync"

	"github.com/beego/beego/v2/core/logs"
)

var stopUDPCh chan struct{}
var wgUDP sync.WaitGroup

var bufferPool = sync.Pool{
	New: func() interface{} {
		buffer := make([]byte, 4096) // 4KB buffer
		return &buffer
	},
}

func StartUDPServer(port int64) {
	if conn, err := net.ListenPacket("udp", fmt.Sprintf(":%d", port)); err != nil {
		logs.Error("Error listening", err.Error())
	} else {
		wgUDP.Add(1)
		defer wgUDP.Done()
		defer conn.Close()

		logs.Info("UDP server listening on port %d...\n", port)
		stopUDPCh = make(chan struct{})

		for i := 0; i < 10; i++ { // 启动多个 goroutine 以提高并发性能
			go handleUDPConnection(conn)
		}
		<-stopUDPCh
		logs.Info("Stopping server...")
	}
}

func handleUDPConnection(conn net.PacketConn) {
	buffer := bufferPool.Get().(*[]byte) // 获取缓冲区
	defer bufferPool.Put(buffer)         // 使用完后归还缓冲区

	for {
		select {
		case <-stopUDPCh:
			logs.Info("Stopping UDP server...")
			return
		default:
			// 接收数据
			n, addr, err := conn.ReadFrom(*buffer)
			if err != nil {
				logs.Error("Error reading from UDP:", err)
				continue
			}

			// 处理数据
			receivedMessage := hex.EncodeToString((*buffer)[:n])
			// 如果接收头是00000014,则转为string
			if len(receivedMessage) > 8 && receivedMessage[:8] == "00000014" {
				receivedMessage = "00000020" + string((*buffer)[4:n])
			}
			logs.Info("Received from %s: %s\n", addr.String(), receivedMessage)

			// 回复客户端
			logs.Info("Echo %s: %s", addr.String(), receivedMessage)
			_, err = conn.WriteTo((*buffer)[:n], addr)
			if err != nil {
				logs.Error("Error sending to UDP:", err)
			}
		}
	}
}

func StopUDPServer() {
	close(stopUDPCh)
	wgUDP.Wait()
	logs.Info("UDP server stopped.")
}
