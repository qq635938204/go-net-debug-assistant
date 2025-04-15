package communication

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/v2/core/logs"
)

type TcpServer struct {
	listener       net.Listener
	mutexConn      sync.Mutex
	connList       map[string]*net.Conn
	messageHandler func(string, []byte)
	closeCh        chan struct{}
	wg             sync.WaitGroup
}

type MultiSendParam struct {
	Addr string
	Data []byte
}

type MultiSendResult struct {
	Addr string
	Resp []byte
	Err  error
}

func (t *TcpServer) Start(port int64, handler func(string, []byte)) {
	t.mutexConn.Lock()
	t.connList = make(map[string]*net.Conn)
	t.mutexConn.Unlock()
	t.messageHandler = handler
	t.closeCh = make(chan struct{})
	defer t.wg.Done()
	if tempListener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port)); err != nil {
		logs.Error("Error listening", err.Error())
	} else {
		t.listener = tempListener
		t.wg.Add(1)
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

func (t *TcpServer) handleRequest(conn net.Conn) {
	t.mutexConn.Lock()
	t.connList[conn.RemoteAddr().String()] = &conn
	t.mutexConn.Unlock()
}

func (t *TcpServer) Stop() {
	logs.Info("Stop tcp server")
	t.mutexConn.Lock()
	defer t.mutexConn.Unlock()
	for _, conn := range t.connList {
		(*conn).Close()
	}
	if err := t.listener.Close(); err != nil {
		logs.Error("Close listener error,", err)
	}
	close(t.closeCh)
	t.wg.Wait()
}

func (t *TcpServer) SendData(addr string, data []byte) error {
	var retErr error
	var findAddr bool
	data = append(data, []byte("\n")...)
	t.mutexConn.Lock()
	defer t.mutexConn.Unlock()
	for _, conn := range t.connList {
		if conn != nil && strings.Contains((*conn).RemoteAddr().String(), addr) {
			findAddr = true
			if err := (*conn).SetWriteDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
				retErr = err
			} else if _, err := (*conn).Write(data); err != nil {
				retErr = err
			}
			break
		}
	}
	if !findAddr {
		retErr = fmt.Errorf("cannot find addr:%s", addr)
	}
	return retErr
}

func (t *TcpServer) SendDataEx(param []MultiSendParam) []MultiSendResult {
	var mutexRet sync.Mutex
	var ret []MultiSendResult
	var findAddr bool
	var sendWg sync.WaitGroup
	for _, p := range param {
		findAddr = false
		t.mutexConn.Lock()
		for _, conn := range t.connList {
			if conn != nil && strings.Contains((*conn).RemoteAddr().String(), p.Addr) {
				findAddr = true
				sendWg.Add(1)
				go func(add string, conn *net.Conn, data []byte) {
					data = append(data, []byte("\n")...)
					defer sendWg.Done()
					if err := (*conn).SetDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
						mutexRet.Lock()
						ret = append(ret, MultiSendResult{Addr: add, Err: err})
						mutexRet.Unlock()
					} else if _, err := (*conn).Write(data); err != nil {
						mutexRet.Lock()
						ret = append(ret, MultiSendResult{Addr: add, Err: err})
						mutexRet.Unlock()
					} else {
						buf := make([]byte, 1024)
						if reqLen, err := (*conn).Read(buf); err != nil {
							mutexRet.Lock()
							ret = append(ret, MultiSendResult{Addr: add, Err: err})
							mutexRet.Unlock()
						} else {
							logs.Info("Receive data from:%s-%s", (*conn).RemoteAddr().String(), string(buf[:reqLen]))
							mutexRet.Lock()
							ret = append(ret, MultiSendResult{Addr: add, Resp: buf[:reqLen]})
							mutexRet.Unlock()
						}
					}
				}(p.Addr, conn, p.Data)
				break
			}
		}
		t.mutexConn.Unlock()
		if !findAddr {
			mutexRet.Lock()
			ret = append(ret, MultiSendResult{Addr: p.Addr, Err: fmt.Errorf("cannot find addr:%s", p.Addr)})
			mutexRet.Unlock()
		}
	}
	sendWg.Wait()
	return ret
}

func (t *TcpServer) SendAndReceiveData(addr string, data []byte) ([]byte, error) {
	var retErr error
	var findAddr bool = false
	var ret []byte
	data = append(data, []byte("\n")...)
	t.mutexConn.Lock()
	defer t.mutexConn.Unlock()
	for _, conn := range t.connList {
		if conn != nil && strings.Contains((*conn).RemoteAddr().String(), addr) {
			findAddr = true
			if err := (*conn).SetWriteDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
				retErr = err
			} else if _, err := (*conn).Write(data); err != nil {
				retErr = err
			} else {
				buf := make([]byte, 1024)
				if reqLen, err := (*conn).Read(buf); err == nil {
					logs.Info("Receive data from:%s-%s", (*conn).RemoteAddr().String(), string(buf[:reqLen]))
					ret = buf[:reqLen]
				}
			}
			break
		}
	}
	if !findAddr {
		retErr = fmt.Errorf("cannot find addr:%s", addr)
	}
	return ret, retErr
}
