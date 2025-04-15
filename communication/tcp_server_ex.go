package communication

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/beego/beego/v2/core/logs"
)

type TcpServerEx struct {
	listener       net.Listener
	mutexConn      sync.Mutex
	connList       map[string]*net.Conn
	messageHandler func(string, []byte) []byte
	closeCh        chan struct{}
	wg             sync.WaitGroup
}

// Start starts a TCP server, listening on the specified port and network(TCP or UDP).
func (t *TcpServerEx) Start(port int64, network string, handler func(string, []byte) []byte) {
	tempNetwork := strings.ToLower(network)
	if tempNetwork != "tcp" && tempNetwork != "udp" {
		logs.Error("Invalid network type")
		return
	}
	t.mutexConn.Lock()
	t.connList = make(map[string]*net.Conn)
	t.mutexConn.Unlock()
	t.messageHandler = handler
	t.closeCh = make(chan struct{})
	if tempListener, err := net.Listen(tempNetwork, fmt.Sprintf("0.0.0.0:%d", port)); err != nil {
		logs.Error("Error listening", err.Error())
	} else {
		logs.Info("Start tcp server on port", port)
		t.listener = tempListener
		t.wg.Add(1)
		defer t.wg.Done()
		for {
			select {
			case <-t.closeCh:
				return
			default:
				if conn, err := t.listener.Accept(); err != nil {
					logs.Error("Error accepting", err.Error())
					return
				} else {
					logs.Info("Accepting from", conn.RemoteAddr().String())
					go t.handleRequest(conn)
				}
			}
		}
	}
}

func (t *TcpServerEx) handleRequest(conn net.Conn) {
	t.mutexConn.Lock()
	t.connList[conn.RemoteAddr().String()] = &conn
	t.mutexConn.Unlock()
	for {
		reader := bufio.NewReader(conn)
		var buf [2048]byte
		if n, err := reader.Read(buf[:]); err != nil {
			logs.Info("read from client failed, err: ", err)
			break
		} else {
			resp := buf[:n]
			if t.messageHandler != nil {
				resp = t.messageHandler(conn.RemoteAddr().String(), buf[:n])
			}
			if _, err := conn.Write(resp); err != nil {
				logs.Info("write from client failed, err: ", err)
				break
			}
		}
	}
	t.mutexConn.Lock()
	if err := conn.Close(); err != nil {
		logs.Error("Close conn error,", err)
	}
	delete(t.connList, conn.RemoteAddr().String())
	t.mutexConn.Unlock()
}

func (t *TcpServerEx) Stop() {
	logs.Info("Stop tcp server")
	t.mutexConn.Lock()
	defer t.mutexConn.Unlock()
	for _, conn := range t.connList {
		if errc := (*conn).Close(); errc != nil {
			logs.Error("Close conn error,", errc)
		}
	}
	if t.listener != nil {
		if errc := t.listener.Close(); errc != nil {
			logs.Error("Close listener error,", errc)
		}
	}
	close(t.closeCh)
	t.wg.Wait()
	logs.Info("Stop tcp server success")
}
